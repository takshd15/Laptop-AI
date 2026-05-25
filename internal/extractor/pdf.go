package extractor

import (
	"fmt"
	"os"
	"strings"
)

// PDFExtractor handles .pdf files using a minimal built-in text-layer parser.
// It scans for parenthesised string literals inside BT…ET text blocks without
// any external dependency. Scanned/image-only PDFs return empty text.
type PDFExtractor struct{}

func (e *PDFExtractor) Extract(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read pdf: %w", err)
	}
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		return "", fmt.Errorf("not a valid PDF file: %s", path)
	}
	return extractPDFText(string(data)), nil
}

// extractPDFText performs a best-effort scan for readable ASCII text inside
// PDF stream objects. It extracts parenthesised string literals from BT…ET
// text blocks. This is sufficient for simple unencrypted text-layer PDFs.
func extractPDFText(text string) string {
	var sb strings.Builder
	inBT := false
	i := 0
	for i < len(text) {
		if i+2 <= len(text) {
			tok := text[i : i+2]
			if tok == "BT" {
				inBT = true
				i += 2
				continue
			}
			if tok == "ET" {
				inBT = false
				sb.WriteByte('\n')
				i += 2
				continue
			}
		}
		if inBT && text[i] == '(' {
			j := i + 1
			for j < len(text) && text[j] != ')' {
				if text[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(text) {
				for _, c := range text[i+1 : j] {
					if c >= 32 && c < 127 {
						sb.WriteRune(c)
					}
				}
				sb.WriteByte(' ')
			}
			i = j + 1
			continue
		}
		i++
	}
	return strings.TrimSpace(sb.String())
}
