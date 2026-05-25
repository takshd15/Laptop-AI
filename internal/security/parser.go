package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ParserLimits controls how aggressively the file parser gates input.
type ParserLimits struct {
	// MaxFileSizeBytes is the hard upper bound on raw file size before extraction.
	// Files larger than this are skipped entirely. Default: 50 MB.
	MaxFileSizeBytes int64

	// MaxExtractedBytes is the upper bound on the extracted text string length.
	// Text beyond this is silently truncated. Default: 10 MB.
	MaxExtractedBytes int

	// ExtractionTimeout is the maximum wall-clock time allowed for a single
	// file extraction. Default: 30 s.
	ExtractionTimeout time.Duration

	// MaxFolderDepth is the maximum directory nesting depth Walk will descend.
	// Depth of the root folder itself is 0. Default: 20.
	MaxFolderDepth int
}

// DefaultLimits returns safe defaults matching the documented requirements.
func DefaultLimits() ParserLimits {
	return ParserLimits{
		MaxFileSizeBytes:  50 * 1024 * 1024, // 50 MB
		MaxExtractedBytes: 10 * 1024 * 1024, // 10 MB
		ExtractionTimeout: 30 * time.Second,
		MaxFolderDepth:    20,
	}
}

// binaryExts lists extensions that are always treated as binary and skipped.
// This is a blocklist in addition to content sniffing.
var binaryExts = map[string]bool{
	".exe": true, ".dll": true, ".so":  true, ".dylib": true,
	".bin": true, ".o":   true, ".obj": true, ".a":     true,
	".lib": true, ".zip": true, ".tar": true, ".gz":    true,
	".bz2": true, ".xz":  true, ".7z":  true, ".rar":   true,
	".jpg": true, ".jpeg":true, ".png": true, ".gif":   true,
	".bmp": true, ".ico": true, ".svg": true, ".webp":  true,
	".mp3": true, ".mp4": true, ".avi": true, ".mov":   true,
	".mkv": true, ".wav": true, ".flac":true,
	".db":  true, ".sqlite": true, ".pyc": true, ".class": true,
}

// IsBinaryExt returns true if the extension is known-binary and should be skipped
// before any content read is attempted.
func IsBinaryExt(ext string) bool {
	return binaryExts[strings.ToLower(ext)]
}

// CheckFileSize returns an error if the file at path exceeds limits.MaxFileSizeBytes.
func (l ParserLimits) CheckFileSize(info os.FileInfo) error {
	if info.Size() > l.MaxFileSizeBytes {
		return fmt.Errorf("file too large (%d bytes, limit %d)", info.Size(), l.MaxFileSizeBytes)
	}
	return nil
}

// TruncateText returns text truncated to at most limits.MaxExtractedBytes, preserving
// valid UTF-8 by not splitting mid-rune.
func (l ParserLimits) TruncateText(text string) string {
	if len(text) <= l.MaxExtractedBytes {
		return text
	}
	s := text[:l.MaxExtractedBytes]
	// Walk back until we have a valid UTF-8 string boundary.
	for len(s) > 0 {
		if isValidUTF8Boundary(s) {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func isValidUTF8Boundary(s string) bool {
	if len(s) == 0 {
		return true
	}
	b := s[len(s)-1]
	// A continuation byte (10xxxxxx) means we're mid-rune.
	return b < 0x80 || b >= 0xC0
}

// ExtractFunc is the signature of any text extraction function.
type ExtractFunc func(path string) (string, error)

// ExtractWithTimeout runs fn(path) and cancels it if it exceeds limits.ExtractionTimeout.
// Returns the extracted text or an error (including a timeout error).
func (l ParserLimits) ExtractWithTimeout(path string, fn ExtractFunc) (string, error) {
	type result struct {
		text string
		err  error
	}

	ctx, cancel := context.WithTimeout(context.Background(), l.ExtractionTimeout)
	defer cancel()

	ch := make(chan result, 1)
	go func() {
		text, err := fn(path)
		ch <- result{text, err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		return l.TruncateText(res.text), nil
	case <-ctx.Done():
		return "", fmt.Errorf("extraction timed out after %s: %s", l.ExtractionTimeout, filepath.Base(path))
	}
}

// CheckDepth returns an error if path is nested deeper than limits.MaxFolderDepth
// relative to root.
func (l ParserLimits) CheckDepth(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("cannot compute depth: %w", err)
	}
	depth := strings.Count(rel, string(filepath.Separator))
	if depth > l.MaxFolderDepth {
		return fmt.Errorf("folder depth %d exceeds limit %d", depth, l.MaxFolderDepth)
	}
	return nil
}

// IsSymlink returns true if the path's os.Lstat reveals a symlink.
// Symlinks are skipped by default unless the caller explicitly resolves them.
func IsSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}
