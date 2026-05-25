package extractor

import "os"

// PlainTextExtractor handles .txt and source code files (.go, .py, .js, .ts).
type PlainTextExtractor struct{}

func (e *PlainTextExtractor) Extract(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MarkdownExtractor handles .md files.
// Returns raw content — markdown syntax is human-readable and useful context for the LLM.
type MarkdownExtractor struct{}

func (e *MarkdownExtractor) Extract(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
