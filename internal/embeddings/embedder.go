package embeddings

// Embedder converts a text string into a dense float vector.
// The vector dimensions must be consistent within a single session —
// mixing dims from different models will break cosine similarity.
type Embedder interface {
	Embed(text string) ([]float32, error)
}
