// Package integration_test contains security integration tests that exercise
// the full indexer pipeline and security controls end-to-end.
package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/takshd15/laptop-ai/internal/audit"
	"github.com/takshd15/laptop-ai/internal/indexer"
	"github.com/takshd15/laptop-ai/internal/security"
)

// — Security test 1 —————————————————————————————————————————————————————————
// Indexing the home directory root is refused outright.

func TestNoWholeHomeIndexing(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	if err := security.ValidateFolder(home); err == nil {
		t.Error("indexing the home directory root should be rejected, but ValidateFolder returned nil")
	}
}

// — Security test 2 —————————————————————————————————————————————————————————
// .env files are never read, embedded, or stored.
// The indexer skips them via the extension filter (not a supported text type)
// and the denylist; either path ensures the file is never in Records.

func TestEnvFileNotIndexed(t *testing.T) {
	folder := t.TempDir()
	dataDir := t.TempDir()

	mustWriteFile(t, filepath.Join(folder, ".env"), "SECRET_KEY=verylongandprivatevalue12345")
	mustWriteFile(t, filepath.Join(folder, "notes.md"), "These are my public notes.")

	result, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("indexer.Run: %v", err)
	}

	for _, rec := range result.Records {
		if filepath.Base(rec.Path) == ".env" {
			t.Errorf(".env was indexed at %s — it must never be indexed", rec.Path)
		}
	}

	// notes.md should be indexed.
	var found bool
	for _, rec := range result.Records {
		if filepath.Base(rec.Path) == "notes.md" {
			found = true
		}
	}
	if !found {
		t.Error("notes.md should have been indexed but was not found in results")
	}
}

// — Security test 3 —————————————————————————————————————————————————————————
// The ReadOnlyEnforcer blocks any network call attempt from AI code.
// This is the structural guard for "cloud disabled".

func TestCloudCallBlockedByReadOnlyEnforcer(t *testing.T) {
	enforcer := &security.ReadOnlyEnforcer{ReadOnly: true}
	err := enforcer.GuardNetworkCall("https://api.openai.com/v1/chat/completions")
	if err == nil {
		t.Error("network call should be blocked by read-only enforcer, but got nil error")
	}
}

func TestCloudCallAllowedWhenReadOnlyOff(t *testing.T) {
	enforcer := &security.ReadOnlyEnforcer{ReadOnly: false}
	err := enforcer.GuardNetworkCall("https://api.openai.com/v1/chat/completions")
	if err != nil {
		t.Errorf("network call should be allowed when ReadOnly=false, got: %v", err)
	}
}

// — Security test 4 —————————————————————————————————————————————————————————
// Symlinked paths are skipped by the indexer and do not expose out-of-tree content.

func TestSymlinkSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows — skipping")
	}

	folder := t.TempDir()
	dataDir := t.TempDir()
	secretDir := t.TempDir()

	// Create a file outside the indexed folder.
	secretFile := filepath.Join(secretDir, "id_rsa")
	mustWriteFile(t, secretFile, "secret SSH private key content here")

	// Create a symlink inside the indexed folder pointing to the secret.
	symlinkPath := filepath.Join(folder, "innocent.txt")
	if err := os.Symlink(secretFile, symlinkPath); err != nil {
		t.Skipf("os.Symlink failed: %v", err)
	}

	// Create a legitimate file that should be indexed.
	mustWriteFile(t, filepath.Join(folder, "notes.txt"), "these are normal notes")

	result, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("indexer.Run: %v", err)
	}

	// The symlink must not be in the indexed records.
	for _, rec := range result.Records {
		if rec.Path == symlinkPath {
			t.Errorf("symlinked file %q was indexed — it must be skipped", symlinkPath)
		}
	}

	// notes.txt should be indexed.
	var found bool
	for _, rec := range result.Records {
		if filepath.Base(rec.Path) == "notes.txt" {
			found = true
		}
	}
	if !found {
		t.Error("notes.txt should be indexed but was not found in results")
	}
}

// — Security test 5 —————————————————————————————————————————————————————————
// Files containing secrets are skipped: not read further, not embedded, not stored.

func TestSecretFilesNotIndexed(t *testing.T) {
	folder := t.TempDir()
	dataDir := t.TempDir()

	// A file containing an OpenAI-style API key (48 alphanumeric chars after sk-).
	secretContent := "API Token: sk-" + repeatStr("X", 48) + "\nDo not share this."
	// Use a name that is not in the denylist so the file reaches the secret
	// scanner (not the denylist filter) and SkippedSecret is incremented.
	mustWriteFile(t, filepath.Join(folder, "api_config.md"), secretContent)
	mustWriteFile(t, filepath.Join(folder, "readme.md"), "This is the project README. No secrets.")

	result, err := indexer.Run(folder, dataDir, []string{folder}, audit.Nop())
	if err != nil {
		t.Fatalf("indexer.Run: %v", err)
	}

	// api_config.md must be excluded by the secret scanner.
	for _, rec := range result.Records {
		if filepath.Base(rec.Path) == "api_config.md" {
			t.Error("api_config.md with API key was indexed — it must be skipped")
		}
	}
	if result.SkippedSecret == 0 {
		t.Error("expected SkippedSecret > 0 for the credentials file")
	}

	// readme.md should be indexed normally.
	var found bool
	for _, rec := range result.Records {
		if filepath.Base(rec.Path) == "readme.md" {
			found = true
		}
	}
	if !found {
		t.Error("readme.md should be indexed but was not found in results")
	}
}

func repeatStr(s string, n int) string {
	result := make([]byte, n*len(s))
	for i := range result {
		result[i] = s[i%len(s)]
	}
	return string(result)
}
