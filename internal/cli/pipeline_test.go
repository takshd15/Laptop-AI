package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takshd15/laptop-ai/internal/embeddings"
	"github.com/takshd15/laptop-ai/internal/indexer"
	"github.com/takshd15/laptop-ai/internal/vectordb"
)

func TestIndexRecordsWithEmbedder_PopulatesVectorDB(t *testing.T) {
	dataDir := t.TempDir()
	notesDir := t.TempDir()
	notePath := filepath.Join(notesDir, "cats.md")
	if err := os.WriteFile(notePath, []byte("Cats like warm windows and quiet rooms."), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	chunks, err := indexRecordsWithEmbedder(dataDir, []indexer.FileRecord{{Path: notePath}}, embeddings.NewMock())
	if err != nil {
		t.Fatalf("indexRecordsWithEmbedder: %v", err)
	}
	if chunks != 1 {
		t.Fatalf("chunks indexed = %d, want 1", chunks)
	}

	db, err := vectordb.Open(vectorDBDir(dataDir))
	if err != nil {
		t.Fatalf("vectordb.Open: %v", err)
	}
	defer db.Close()
	if got := db.Count(); got != 1 {
		t.Fatalf("vector DB count = %d, want 1", got)
	}
}
