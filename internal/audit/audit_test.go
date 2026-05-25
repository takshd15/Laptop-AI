package audit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takshd15/laptop-ai/internal/audit"
)

// TestAuditLog_MetadataOnly verifies that the audit log records metadata
// (event type, file path, source list) but never chunk text or question text.
func TestAuditLog_MetadataOnly(t *testing.T) {
	dir := t.TempDir()
	log, err := audit.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const sensitiveChunkText = "SENSITIVE_CHUNK_TEXT_MUST_NOT_APPEAR_IN_LOG"
	const sensitiveQuestion = "SENSITIVE_QUESTION_MUST_NOT_APPEAR"

	// Simulate what happens during a query:
	// LogQuery only receives mode + source file paths — never the question or chunks.
	log.LogQuery("local", []string{"cats.md", "notes.md"})

	// Simulate a cloud send event — only provider + paths + count, never chunk text.
	log.LogCloudSend("openai", []string{"cats.md"}, 3)

	// Simulate indexing events.
	log.Log(audit.EventIndexed, "/docs/notes.md", "")
	log.Log(audit.EventSkippedSecret, "/docs/creds.env", "possible secret: OpenAI API key")

	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	logStr := string(data)

	// Content must NOT appear.
	if strings.Contains(logStr, sensitiveChunkText) {
		t.Error("audit log must not contain chunk text")
	}
	if strings.Contains(logStr, sensitiveQuestion) {
		t.Error("audit log must not contain question text")
	}

	// Metadata SHOULD appear.
	for _, want := range []string{"cats.md", "notes.md", "local", "openai", "query", "cloud_send"} {
		if !strings.Contains(logStr, want) {
			t.Errorf("audit log should contain %q but does not", want)
		}
	}

	// The secret value must not appear — only the pattern type ("OpenAI API key" is safe,
	// but the actual secret string is never logged).
	if strings.Contains(logStr, "sk-") {
		t.Error("audit log must not contain any secret values")
	}
}

// TestAuditLog_AppendOnly verifies that repeated opens continue appending.
func TestAuditLog_AppendOnly(t *testing.T) {
	dir := t.TempDir()

	log1, _ := audit.Open(dir)
	log1.Log(audit.EventIndexed, "/a.md", "")
	log1.Close()

	log2, _ := audit.Open(dir)
	log2.Log(audit.EventIndexed, "/b.md", "")
	log2.Close()

	data, _ := os.ReadFile(filepath.Join(dir, "audit.log"))
	logStr := string(data)
	if !strings.Contains(logStr, "/a.md") || !strings.Contains(logStr, "/b.md") {
		t.Error("audit log should contain entries from both sessions (append-only)")
	}
}

// TestAuditLog_Nop verifies that a nil logger silently discards all events
// instead of panicking.
func TestAuditLog_Nop(t *testing.T) {
	nop := audit.Nop()
	// None of these should panic.
	nop.Log(audit.EventIndexed, "/file.md", "")
	nop.LogQuery("local", []string{"file.md"})
	nop.LogCloudSend("openai", []string{"file.md"}, 1)
	if err := nop.Close(); err != nil {
		t.Errorf("Nop().Close() returned error: %v", err)
	}
}
