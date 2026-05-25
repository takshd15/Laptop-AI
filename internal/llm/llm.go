package llm

import "context"

// Chunk is a retrieved text segment passed to the LLM as context.
// It comes from the vector DB search results.
type Chunk struct {
	Text     string
	FilePath string
	Score    float32 // cosine similarity — not sent to the LLM, used by the caller
}

// LLM generates natural-language answers from retrieved context chunks.
// The vector DB finds the information; the LLM explains it.
// These are separate concerns and must stay that way.
type LLM interface {
	// Answer waits for the full response and returns it as a string.
	Answer(ctx context.Context, question string, chunks []Chunk) (string, error)

	// Stream calls out for each token as it arrives — use for live terminal output.
	Stream(ctx context.Context, question string, chunks []Chunk, out func(token string)) error
}
