package security

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// CloudSendRequest describes a batch of chunks about to be sent to a cloud model.
type CloudSendRequest struct {
	Provider string
	Chunks   []CloudChunk
}

// CloudChunk is a sanitised view of one context chunk for cloud preview.
type CloudChunk struct {
	Index    int
	FilePath string
	// Text is shown in the preview but secrets are redacted before display and send.
	Text string
}

// CloudSendLog is the metadata written to the audit log — no content, just provenance.
type CloudSendLog struct {
	Timestamp string
	Provider  string
	FilePaths []string
	ChunkCount int
}

// RedactSecrets replaces any secret-pattern matches in text with a placeholder.
// The returned string is safe to display and safe to send to a cloud model.
func RedactSecrets(text string) string {
	matches := ScanForSecrets(text)
	if len(matches) == 0 {
		return text
	}
	// Re-run each compiled pattern and replace matched bytes.
	// We use the same compiled regexes from secrets.go via secretPatterns.
	out := text
	for _, p := range secretPatterns {
		out = p.pattern.ReplaceAllStringFunc(out, func(m string) string {
			return "[REDACTED:" + p.name + "]"
		})
	}
	return out
}

// truncateRune truncates s to at most maxBytes, preserving valid UTF-8.
func truncateRune(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// PreviewAndConfirm prints a preview of every chunk that would be sent to the
// cloud provider and asks the user for explicit confirmation. Returns true if
// the user confirms, false if they decline.
//
// Safety steps performed here:
//  1. Secrets are redacted in the preview text and in the returned chunks.
//  2. Every source file path is listed before confirmation.
//  3. The question is defaulted to NO so an accidental Enter does not send.
func PreviewAndConfirm(req *CloudSendRequest) (sanitised *CloudSendRequest, confirmed bool) {
	fmt.Printf("\n=== CLOUD SEND PREVIEW ===\n")
	fmt.Printf("Provider : %s\n", req.Provider)
	fmt.Printf("Chunks   : %d\n\n", len(req.Chunks))

	// Show source files (unique list)
	seen := map[string]bool{}
	fmt.Println("Source files that will be sent:")
	for _, c := range req.Chunks {
		if !seen[c.FilePath] {
			fmt.Printf("  • %s\n", c.FilePath)
			seen[c.FilePath] = true
		}
	}
	fmt.Println()

	// Redact and show each chunk
	sanitisedChunks := make([]CloudChunk, len(req.Chunks))
	for i, c := range req.Chunks {
		redacted := RedactSecrets(c.Text)
		// Cap preview at 300 chars to keep terminal readable
		preview := truncateRune(redacted, 300)
		if len(redacted) > 300 {
			preview += " …"
		}
		fmt.Printf("[%d] %s\n%s\n\n", i+1, c.FilePath, preview)
		sanitisedChunks[i] = CloudChunk{
			Index:    c.Index,
			FilePath: c.FilePath,
			Text:     redacted,
		}
	}

	fmt.Printf("WARNING: The text above will be sent to %s (external service).\n", req.Provider)
	fmt.Print("Confirm? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
		fmt.Println("Cancelled. Nothing was sent.")
		return nil, false
	}

	return &CloudSendRequest{Provider: req.Provider, Chunks: sanitisedChunks}, true
}

// BuildCloudSendLog creates the audit record for a confirmed cloud send.
// Records provider, timestamp, and file paths only — never chunk text.
func BuildCloudSendLog(req *CloudSendRequest) CloudSendLog {
	paths := make([]string, 0, len(req.Chunks))
	seen := map[string]bool{}
	for _, c := range req.Chunks {
		if !seen[c.FilePath] {
			paths = append(paths, c.FilePath)
			seen[c.FilePath] = true
		}
	}
	return CloudSendLog{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Provider:   req.Provider,
		FilePaths:  paths,
		ChunkCount: len(req.Chunks),
	}
}
