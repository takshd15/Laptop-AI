package chunker_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/takshd15/laptop-ai/internal/chunker"
)

// make1000Words builds a deterministic 1000-word string of the form "word0000 word0001 …".
func make1000Words() string {
	words := make([]string, 1000)
	for i := range words {
		words[i] = fmt.Sprintf("word%04d", i)
	}
	return strings.Join(words, " ")
}

func TestChunker_1000Words_MultipleChunks(t *testing.T) {
	c := chunker.New()
	chunks := c.Chunk(make1000Words(), "test.md")

	// ChunkSize=500, Overlap=80 → step=420
	// chunk 0: words[0:500], chunk 1: words[420:920], chunk 2: words[840:1000]
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for 1000 words, got %d", len(chunks))
	}
}

func TestChunker_1000Words_CorrectOverlap(t *testing.T) {
	c := chunker.New()
	chunks := c.Chunk(make1000Words(), "test.md")

	if len(chunks) < 2 {
		t.Skip("need at least 2 chunks to test overlap")
	}

	overlap := chunker.DefaultOverlap
	for i := 0; i < len(chunks)-1; i++ {
		w1 := strings.Fields(chunks[i].Text)
		w2 := strings.Fields(chunks[i+1].Text)
		if len(w1) < overlap || len(w2) < overlap {
			continue // final short chunk may have fewer words
		}
		tail := w1[len(w1)-overlap:]
		head := w2[:overlap]
		for j := range tail {
			if tail[j] != head[j] {
				t.Errorf("chunk %d→%d overlap word %d: got %q, want %q",
					i, i+1, j, head[j], tail[j])
				break
			}
		}
	}
}

func TestChunker_1000Words_NoEmptyChunks(t *testing.T) {
	c := chunker.New()
	for i, ch := range c.Chunk(make1000Words(), "test.md") {
		if strings.TrimSpace(ch.Text) == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestChunker_1000Words_MetadataPreserved(t *testing.T) {
	c := chunker.New()
	const filePath = "/some/path/test.md"
	for i, ch := range c.Chunk(make1000Words(), filePath) {
		if ch.FilePath != filePath {
			t.Errorf("chunk %d: FilePath = %q, want %q", i, ch.FilePath, filePath)
		}
		if ch.StartLine <= 0 {
			t.Errorf("chunk %d: StartLine = %d, want > 0", i, ch.StartLine)
		}
		if ch.EndLine < ch.StartLine {
			t.Errorf("chunk %d: EndLine %d < StartLine %d", i, ch.EndLine, ch.StartLine)
		}
		if ch.ID == "" {
			t.Errorf("chunk %d: ID is empty", i)
		}
		if ch.ContentHash == "" {
			t.Errorf("chunk %d: ContentHash is empty", i)
		}
	}
}

func TestChunker_EmptyText_ReturnsNil(t *testing.T) {
	c := chunker.New()
	if chunks := c.Chunk("", "test.md"); chunks != nil {
		t.Errorf("expected nil for empty text, got %d chunks", len(chunks))
	}
}

func TestChunker_WhitespaceOnly_ReturnsNil(t *testing.T) {
	c := chunker.New()
	if chunks := c.Chunk("   \n\t  \n  ", "test.md"); chunks != nil {
		t.Errorf("expected nil for whitespace-only text, got %d chunks", len(chunks))
	}
}

func TestChunker_ShortText_SingleChunk(t *testing.T) {
	c := chunker.New()
	text := "This is a short document with fewer than five hundred words."
	chunks := c.Chunk(text, "short.md")
	if len(chunks) != 1 {
		t.Errorf("short text should produce exactly 1 chunk, got %d", len(chunks))
	}
}
