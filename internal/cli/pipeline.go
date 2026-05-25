package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/takshd15/laptop-ai/internal/chunker"
	"github.com/takshd15/laptop-ai/internal/embeddings"
	"github.com/takshd15/laptop-ai/internal/extractor"
	"github.com/takshd15/laptop-ai/internal/indexer"
	"github.com/takshd15/laptop-ai/internal/llm"
	"github.com/takshd15/laptop-ai/internal/vectordb"
)

const defaultTopK = 5

func vectorDBDir(dataDir string) string {
	return filepath.Join(dataDir, "vectors")
}

func indexRecords(dataDir string, records []indexer.FileRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	return indexRecordsWithEmbedder(dataDir, records, embeddings.NewLocal())
}

func vectorRecordCount(dataDir string) (int, error) {
	db, err := vectordb.Open(vectorDBDir(dataDir))
	if err != nil {
		return 0, fmt.Errorf("cannot open vector DB: %w", err)
	}
	defer db.Close()
	return db.Count(), nil
}

func indexRecordsWithEmbedder(dataDir string, records []indexer.FileRecord, emb embeddings.Embedder) (int, error) {
	db, err := vectordb.Open(vectorDBDir(dataDir))
	if err != nil {
		return 0, fmt.Errorf("cannot open vector DB: %w", err)
	}
	defer db.Close()

	ck := chunker.New()
	totalChunks := 0
	for _, rec := range records {
		ext, err := extractor.ForFile(rec.Path)
		if err != nil {
			return totalChunks, fmt.Errorf("cannot choose extractor for %s: %w", rec.Path, err)
		}

		text, err := ext.Extract(rec.Path)
		if err != nil {
			return totalChunks, fmt.Errorf("cannot extract %s: %w", rec.Path, err)
		}
		if strings.TrimSpace(text) == "" {
			fmt.Printf("  chunks %s (0, empty text)\n", rec.Path)
			continue
		}

		chunks := ck.Chunk(text, rec.Path)
		for _, ch := range chunks {
			vec, err := emb.Embed(ch.Text)
			if err != nil {
				return totalChunks, fmt.Errorf("cannot embed %s chunk %s: %w", rec.Path, ch.ID, err)
			}
			_, err = db.Insert(vec, ch.Text, map[string]string{
				"file":       rec.Path,
				"chunk_id":   ch.ID,
				"start_line": strconv.Itoa(ch.StartLine),
				"end_line":   strconv.Itoa(ch.EndLine),
			})
			if err != nil {
				return totalChunks, fmt.Errorf("cannot store %s chunk %s: %w", rec.Path, ch.ID, err)
			}
			totalChunks++
		}
		fmt.Printf("  chunks %s (%d)\n", rec.Path, len(chunks))
	}

	if err := db.Flush(); err != nil {
		return totalChunks, fmt.Errorf("cannot flush vector DB: %w", err)
	}
	return totalChunks, nil
}

func searchChunks(dataDir, question string, topK int) ([]llm.Chunk, error) {
	emb := embeddings.NewLocal()
	queryVec, err := emb.Embed(question)
	if err != nil {
		return nil, fmt.Errorf("cannot embed question: %w", err)
	}

	db, err := vectordb.Open(vectorDBDir(dataDir))
	if err != nil {
		return nil, fmt.Errorf("cannot open vector DB: %w", err)
	}
	defer db.Close()

	results, err := db.Search(queryVec, topK)
	if err != nil {
		return nil, fmt.Errorf("cannot search vector DB: %w", err)
	}

	chunks := make([]llm.Chunk, 0, len(results))
	for _, r := range results {
		chunks = append(chunks, llm.Chunk{
			Text:     r.Record.Text,
			FilePath: r.Record.Metadata["file"],
			Score:    r.Score,
		})
	}
	return chunks, nil
}

func uniqueSourcePaths(chunks []llm.Chunk) []string {
	seen := make(map[string]bool, len(chunks))
	paths := make([]string, 0, len(chunks))
	for _, ch := range chunks {
		if ch.FilePath == "" || seen[ch.FilePath] {
			continue
		}
		seen[ch.FilePath] = true
		paths = append(paths, ch.FilePath)
	}
	return paths
}
