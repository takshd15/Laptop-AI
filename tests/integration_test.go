// Package integration_test exercises the full laptop-ai pipeline end-to-end
// without requiring a running Ollama instance. A keyword-based stub embedder
// provides deterministic, semantically meaningful vectors so ranking assertions
// are reliable. A stub LLM echoes the top retrieved chunk.
package integration_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takshd15/laptop-ai/internal/audit"
	"github.com/takshd15/laptop-ai/internal/chunker"
	"github.com/takshd15/laptop-ai/internal/extractor"
	"github.com/takshd15/laptop-ai/internal/indexer"
	"github.com/takshd15/laptop-ai/internal/llm"
	"github.com/takshd15/laptop-ai/internal/vectordb"
)

// — test doubles ——————————————————————————————————————————————————————————————

// keywordEmbedder returns an L2-normalised vector where each dimension
// corresponds to whether a keyword appears in the input text.
// This makes ranking deterministic and semantically correct for tests.
type keywordEmbedder struct {
	keywords []string
}

func newKeywordEmbedder() *keywordEmbedder {
	return &keywordEmbedder{
		keywords: []string{
			"milk", "cat", "sleep", "drink",
			"car", "engine", "wheel",
			"kitten", "animal",
		},
	}
}

func (e *keywordEmbedder) Embed(text string) ([]float32, error) {
	lower := strings.ToLower(text)
	vec := make([]float32, len(e.keywords))
	for i, kw := range e.keywords {
		if strings.Contains(lower, kw) {
			vec[i] = 1.0
		}
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum > 0 {
		norm := float32(math.Sqrt(sum))
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec, nil
}

// echoLLM returns the text of the top-ranked chunk as its "answer".
type echoLLM struct{}

func (e *echoLLM) Answer(_ context.Context, _ string, chunks []llm.Chunk) (string, error) {
	if len(chunks) == 0 {
		return "No context found.", nil
	}
	return chunks[0].Text, nil
}

func (e *echoLLM) Stream(_ context.Context, _ string, chunks []llm.Chunk, out func(string)) error {
	if len(chunks) > 0 {
		out(chunks[0].Text)
	}
	return nil
}

// — pipeline helper ———————————————————————————————————————————————————————————

type embedFunc interface {
	Embed(text string) ([]float32, error)
}

// buildIndex runs index → extract → chunk → embed → insert for every file in
// folder and returns the populated vector DB. The DB is the caller's to close.
func buildIndex(t *testing.T, folder, dataDir string, emb embedFunc) *vectordb.DB {
	t.Helper()

	db, err := vectordb.Open(filepath.Join(dataDir, "vectors"))
	if err != nil {
		t.Fatalf("vectordb.Open: %v", err)
	}

	result, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("indexer.Run: %v", err)
	}

	ck := chunker.New()
	for _, rec := range result.Records {
		ext, err := extractor.ForFile(rec.Path)
		if err != nil {
			t.Fatalf("extractor.ForFile(%s): %v", rec.Path, err)
		}
		text, err := ext.Extract(rec.Path)
		if err != nil {
			t.Fatalf("Extract(%s): %v", rec.Path, err)
		}
		for _, ch := range ck.Chunk(text, rec.Path) {
			vec, err := emb.Embed(ch.Text)
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			if _, err := db.Insert(vec, ch.Text, map[string]string{
				"file":     rec.Path,
				"chunk_id": ch.ID,
			}); err != nil {
				t.Fatalf("db.Insert: %v", err)
			}
		}
	}
	return db
}

// mustWriteFile writes content to path, failing the test on error.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// — integration tests ————————————————————————————————————————————————————————

// TestIndexAndSearch verifies that after indexing two documents the vector DB
// returns the semantically closer document first.
func TestIndexAndSearch(t *testing.T) {
	folder := t.TempDir()
	dataDir := t.TempDir()

	mustWriteFile(t, filepath.Join(folder, "cats.md"), "Cats like sleeping and drinking milk.")
	mustWriteFile(t, filepath.Join(folder, "cars.md"), "Cars have engines and wheels.")

	emb := newKeywordEmbedder()
	db := buildIndex(t, folder, dataDir, emb)
	defer db.Close()

	queryVec, err := emb.Embed("kitten milk")
	if err != nil {
		t.Fatalf("Embed query: %v", err)
	}
	results, err := db.Search(queryVec, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned no results")
	}

	topFile := results[0].Record.Metadata["file"]
	if !strings.HasSuffix(topFile, "cats.md") {
		t.Errorf("top result should be cats.md, got %q (score=%.4f)", topFile, results[0].Score)
	}
}

// TestAskCommand verifies the full ask pipeline: search → LLM → answer + source.
func TestAskCommand(t *testing.T) {
	folder := t.TempDir()
	dataDir := t.TempDir()

	mustWriteFile(t, filepath.Join(folder, "cats.md"), "Cats like sleeping and drinking milk.")
	mustWriteFile(t, filepath.Join(folder, "cars.md"), "Cars have engines and wheels.")

	emb := newKeywordEmbedder()
	db := buildIndex(t, folder, dataDir, emb)
	defer db.Close()

	queryVec, err := emb.Embed("what animals like milk")
	if err != nil {
		t.Fatalf("Embed query: %v", err)
	}
	results, err := db.Search(queryVec, 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned no results")
	}

	// Convert DB results to LLM chunks.
	llmChunks := make([]llm.Chunk, len(results))
	for i, r := range results {
		llmChunks[i] = llm.Chunk{
			Text:     r.Record.Text,
			FilePath: r.Record.Metadata["file"],
			Score:    r.Score,
		}
	}

	answer, err := (&echoLLM{}).Answer(context.Background(), "what animals like milk?", llmChunks)
	if err != nil {
		t.Fatalf("LLM.Answer: %v", err)
	}

	// The echoLLM returns the top chunk's text; that chunk must come from cats.md.
	answerLower := strings.ToLower(answer)
	if !strings.Contains(answerLower, "cat") && !strings.Contains(answerLower, "milk") {
		t.Errorf("answer should mention cats or milk, got: %q", answer)
	}

	topSource := llmChunks[0].FilePath
	if !strings.HasSuffix(topSource, "cats.md") {
		t.Errorf("top source should be cats.md, got %q", topSource)
	}
}

// TestReindexChangedFile verifies the indexer's hash-based change detection:
//
//   - First run:  new file → Indexed = 1
//   - Second run: same file → Skipped ≥ 1, Indexed = 0
//   - Third run:  file modified → Indexed = 1 again
func TestReindexChangedFile(t *testing.T) {
	folder := t.TempDir()
	dataDir := t.TempDir()
	filePath := filepath.Join(folder, "doc.md")

	// First index — file is new.
	mustWriteFile(t, filePath, "Initial content for indexing.")
	r1, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if r1.Indexed != 1 {
		t.Errorf("first run: Indexed = %d, want 1", r1.Indexed)
	}

	// Second index immediately — file unchanged.
	r2, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if r2.Indexed != 0 {
		t.Errorf("second run: Indexed = %d, want 0 (unchanged)", r2.Indexed)
	}
	if r2.Skipped == 0 {
		t.Error("second run: expected at least 1 skipped file")
	}

	// Modify the file, third index — change detected.
	mustWriteFile(t, filePath, "Modified content that is completely different from before.")
	r3, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if r3.Indexed != 1 {
		t.Errorf("third run: Indexed = %d, want 1 (change detected)", r3.Indexed)
	}
}
