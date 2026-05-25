package vectordb

import (
	"math"
	"testing"
)

func TestCosineSimilarity_SameVector(t *testing.T) {
	v := []float32{0.6, 0.8} // already unit-length
	norm := l2Norm(v)
	score := cosineSimilarity(v, norm, v, norm)
	if math.Abs(float64(score)-1.0) > 1e-6 {
		t.Errorf("same vector: got %f, want 1.0", score)
	}
}

func TestCosineSimilarity_OppositeVector(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{-1.0, 0.0}
	score := cosineSimilarity(a, l2Norm(a), b, l2Norm(b))
	if math.Abs(float64(score)+1.0) > 1e-6 {
		t.Errorf("opposite vector: got %f, want -1.0", score)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{0.0, 1.0}
	score := cosineSimilarity(a, 1.0, b, 1.0)
	if math.Abs(float64(score)) > 1e-6 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", score)
	}
}

func TestCosineSimilarity_ZeroNorm(t *testing.T) {
	a := []float32{1.0, 0.0}
	zero := []float32{0.0, 0.0}
	score := cosineSimilarity(a, l2Norm(a), zero, l2Norm(zero))
	if score != 0 {
		t.Errorf("zero-norm vector: got %f, want 0.0", score)
	}
}

// TestDBSearch_EmptyQuery verifies that searching with an empty vector is rejected.
func TestDBSearch_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, err = db.Search([]float32{}, 5)
	if err == nil {
		t.Fatal("expected error for empty query vector, got nil")
	}
}

// TestDBSearch_DimensionMismatch verifies that a query with the wrong number of
// dimensions produces no results and does not crash.
func TestDBSearch_DimensionMismatch(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Insert a 3-dimensional record.
	_, err = db.Insert([]float32{1.0, 0.0, 0.0}, "3D record", nil)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Search with a 2-dimensional query — dimensions do not match.
	// topKSearch skips mismatched records; the result should be empty, not a panic.
	results, err := db.Search([]float32{1.0, 0.0}, 5)
	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("dimension mismatch: expected 0 results, got %d", len(results))
	}
}
