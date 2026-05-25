package vectordb

import "math"

func dotProduct(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func l2Norm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

// cosineSimilarity returns a score in [-1, 1].
// Pre-computed norms are passed in to avoid recomputing them for every comparison during search.
func cosineSimilarity(a []float32, normA float32, b []float32, normB float32) float32 {
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct(a, b) / (normA * normB)
}
