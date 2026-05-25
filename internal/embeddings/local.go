package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	DefaultOllamaURL = "http://localhost:11434"
	DefaultModel     = "nomic-embed-text"
)

// LocalEmbedder calls Ollama's REST API to embed text using a local model.
// Requires Ollama to be running: `ollama serve`
// Requires the model to be pulled: `ollama pull nomic-embed-text`
type LocalEmbedder struct {
	Model  string
	APIURL string
	client *http.Client
}

func NewLocal() *LocalEmbedder {
	return &LocalEmbedder{
		Model:  DefaultModel,
		APIURL: DefaultOllamaURL,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (e *LocalEmbedder) Embed(text string) ([]float32, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: e.Model, Prompt: text})
	if err != nil {
		return nil, err
	}

	resp, err := e.client.Post(e.APIURL+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable at %s — run: ollama serve\n  %w", e.APIURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned HTTP %d — run: ollama pull %s", resp.StatusCode, e.Model)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("cannot decode ollama response: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding for model %q", e.Model)
	}
	return result.Embedding, nil
}
