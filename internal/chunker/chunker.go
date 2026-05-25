package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultChunkSize = 500 // words per chunk
	DefaultOverlap   = 80  // words shared between adjacent chunks
)

// Chunk is a single slice of text extracted from a file, ready for embedding.
type Chunk struct {
	ID          string    `json:"chunk_id"`
	FilePath    string    `json:"file_path"`
	Text        string    `json:"text"`
	StartLine   int       `json:"start_line"`
	EndLine     int       `json:"end_line"`
	CreatedAt   time.Time `json:"created_at"`
	ContentHash string    `json:"content_hash"`
}

// Chunker splits text into overlapping windows of words.
type Chunker struct {
	ChunkSize int
	Overlap   int
}

func New() *Chunker {
	return &Chunker{
		ChunkSize: DefaultChunkSize,
		Overlap:   DefaultOverlap,
	}
}

type wordEntry struct {
	word string
	line int // 1-indexed
}

// Chunk splits text into overlapping word-window chunks.
// Each chunk knows which source lines it covers so we can cite the file later.
func (c *Chunker) Chunk(text, filePath string) []Chunk {
	lines := strings.Split(text, "\n")

	var words []wordEntry
	for lineIdx, line := range lines {
		for _, w := range strings.Fields(line) {
			words = append(words, wordEntry{word: w, line: lineIdx + 1})
		}
	}

	if len(words) == 0 {
		return nil
	}

	step := c.ChunkSize - c.Overlap
	if step <= 0 {
		step = 1
	}

	now := time.Now().UTC()
	var chunks []Chunk

	for i := 0; i < len(words); i += step {
		end := i + c.ChunkSize
		if end > len(words) {
			end = len(words)
		}

		slice := words[i:end]

		parts := make([]string, len(slice))
		for j, w := range slice {
			parts[j] = w.word
		}
		chunkText := strings.Join(parts, " ")

		startLine := slice[0].line
		endLine := slice[len(slice)-1].line

		contentHash := hash(chunkText)
		// ID is stable: same file + same line range → same ID across re-runs
		chunkID := hash(fmt.Sprintf("%s:%d:%d", filePath, startLine, endLine))

		chunks = append(chunks, Chunk{
			ID:          chunkID,
			FilePath:    filePath,
			Text:        chunkText,
			StartLine:   startLine,
			EndLine:     endLine,
			CreatedAt:   now,
			ContentHash: contentHash,
		})

		if end == len(words) {
			break
		}
	}

	return chunks
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
