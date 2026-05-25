package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event types — what happened to a file during indexing.
const (
	EventIndexed          = "indexed"
	EventSkippedDenied    = "skipped_denylist"
	EventSkippedSecret    = "skipped_secret"
	EventSkippedAllowed   = "skipped_not_allowed"
	EventSkippedUnchanged = "skipped_unchanged"
	EventSkippedBinary    = "skipped_binary"
	EventSkippedTooLarge  = "skipped_too_large"
	EventSkippedDepth     = "skipped_depth"
	EventSkippedSymlink   = "skipped_symlink"
	EventSkippedTimeout   = "skipped_timeout"
	EventError            = "error"
	EventQuery            = "query"
	EventCloudSend        = "cloud_send"
)

// entry is one audit log record.
// IMPORTANT: never include file content or secret values here.
// Only paths, event types, and human-readable reasons are safe to log.
type entry struct {
	Timestamp string `json:"ts"`
	Event     string `json:"event"`
	Path      string `json:"path,omitempty"`
	Reason    string `json:"reason,omitempty"`

	// Query event fields
	Mode       string   `json:"mode,omitempty"`
	TopSources []string `json:"top_sources,omitempty"`

	// Cloud send event fields
	Provider   string   `json:"provider,omitempty"`
	FilePaths  []string `json:"file_paths,omitempty"`
	ChunkCount int      `json:"chunk_count,omitempty"`
}

// Logger appends security events to an append-only audit log file.
// Thread-safe.
type Logger struct {
	mu   sync.Mutex
	f    *os.File
}

// Open creates or opens the audit log at dataDir/audit.log.
func Open(dataDir string) (*Logger, error) {
	path := filepath.Join(dataDir, "audit.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot open audit log: %w", err)
	}
	return &Logger{f: f}, nil
}

// Log records a security event. Silently drops the entry if the log file
// is unavailable — audit failures must never block indexing.
func (l *Logger) Log(event, path, reason string) {
	if l == nil {
		return
	}
	e := entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Event:     event,
		Path:      path,
		Reason:    reason,
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.Write(append(line, '\n'))
}

// LogQuery records a question/answer event. Logs the inference mode (local/cloud)
// and the source file paths that contributed to the answer — never the question
// text or the answer text, as those may contain personal information.
func (l *Logger) LogQuery(mode string, topSources []string) {
	if l == nil {
		return
	}
	e := entry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Event:      EventQuery,
		Mode:       mode,
		TopSources: topSources,
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.Write(append(line, '\n'))
}

// LogCloudSend records that chunks were sent to a cloud provider after user
// confirmation. Logs provider name, timestamp, unique source file paths, and
// chunk count — never the chunk text.
func (l *Logger) LogCloudSend(provider string, filePaths []string, chunkCount int) {
	if l == nil {
		return
	}
	e := entry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Event:      EventCloudSend,
		Provider:   provider,
		FilePaths:  filePaths,
		ChunkCount: chunkCount,
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.Write(append(line, '\n'))
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	return l.f.Close()
}

// Nop returns a logger that discards all events.
// Use in tests or when auditing is not configured.
func Nop() *Logger {
	return nil
}
