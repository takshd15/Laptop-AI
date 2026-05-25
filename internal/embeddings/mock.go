package embeddings

import (
	"crypto/sha256"
	"math"
)

const MockDims = 384

// MockEmbedder produces deterministic fake vectors seeded from text content.
// Used for testing the pipeline without a running Ollama instance.
// Same text always produces the same vector; different text produces different vectors.
// These vectors are NOT semantically meaningful — use LocalEmbedder for real search.
type MockEmbedder struct {
	Dims int
}

func NewMock() *MockEmbedder {
	return &MockEmbedder{Dims: MockDims}
}

func (e *MockEmbedder) Embed(text string) ([]float32, error) {
	hash := sha256.Sum256([]byte(text))

	// Seed a PCG-style LCG from the hash bytes so we get good distribution
	// across all dimensions, not just the first 32.
	var seed uint64
	for _, b := range hash {
		seed = seed*31 + uint64(b)
	}

	vec := make([]float32, e.Dims)
	for i := range vec {
		// PCG step: produces a well-distributed sequence
		seed = seed*6364136223846793005 + 1442695040888963407
		// Map the high bits to [-1, 1]
		vec[i] = float32(int64(seed>>33)) / float32(1<<31)
	}

	l2Normalize(vec)
	return vec, nil
}

func l2Normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
}
