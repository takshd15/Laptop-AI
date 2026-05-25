package extractor

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Extractor reads a file and returns its content as plain text.
type Extractor interface {
	Extract(path string) (string, error)
}

// ForFile returns the right Extractor for the given file path based on extension.
func ForFile(path string) (Extractor, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".go", ".py", ".js", ".ts":
		return &PlainTextExtractor{}, nil
	case ".md":
		return &MarkdownExtractor{}, nil
	case ".pdf":
		return &PDFExtractor{}, nil
	case ".docx":
		return &DocxExtractor{}, nil
	case ".html", ".htm":
		return &HTMLExtractor{}, nil
	default:
		return nil, fmt.Errorf("no extractor for extension %q", ext)
	}
}
