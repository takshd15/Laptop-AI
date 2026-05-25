package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultLLMModel  = "llama3"
	DefaultOllamaURL = "http://localhost:11434"
)

// OllamaLLM calls the local Ollama server to generate answers.
// Requires Ollama running: `ollama serve`
// Requires the model pulled:  `ollama pull llama3`
//
// Uses streaming by default — tokens arrive as they are generated,
// so Answer() and Stream() both use the streaming endpoint internally.
// This avoids a long silent wait on the HTTP response.
type OllamaLLM struct {
	Model  string
	APIURL string
	client *http.Client
}

func NewOllama() *OllamaLLM {
	return &OllamaLLM{
		Model:  DefaultLLMModel,
		APIURL: DefaultOllamaURL,
		// 5-minute timeout: local LLMs on CPU can be slow for long prompts
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// Ollama streams one JSON object per line while generating.
// {"model":"llama3","response":"The ","done":false}
// {"model":"llama3","response":"answer","done":false}
// {"model":"llama3","response":"","done":true,...}
type generateChunk struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Answer streams the response internally and returns the full text once done.
// Use Stream() if you want to print tokens as they arrive.
func (o *OllamaLLM) Answer(ctx context.Context, question string, chunks []Chunk) (string, error) {
	var sb strings.Builder
	err := o.Stream(ctx, question, chunks, func(token string) {
		sb.WriteString(token)
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sb.String()), nil
}

// Stream calls out(token) for each token as Ollama generates it.
// Use this for live terminal output so the user sees progress immediately.
func (o *OllamaLLM) Stream(ctx context.Context, question string, chunks []Chunk, out func(token string)) error {
	prompt := buildPrompt(question, chunks)

	body, err := json.Marshal(generateRequest{
		Model:  o.Model,
		Prompt: prompt,
		Stream: true,
	})
	if err != nil {
		return fmt.Errorf("cannot marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.APIURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s — run: ollama serve\n  %w", o.APIURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned HTTP %d — run: ollama pull %s", resp.StatusCode, o.Model)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk generateChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue // malformed line — skip, not fatal
		}
		if chunk.Response != "" {
			out(chunk.Response)
		}
		if chunk.Done {
			break
		}
	}
	return scanner.Err()
}
