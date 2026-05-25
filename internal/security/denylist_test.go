package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takshd15/laptop-ai/internal/security"
)

func TestDenyRules_EnvSkipped(t *testing.T) {
	if !security.IsDenied(".env", security.DefaultDenyPatterns) {
		t.Error(".env should be in the denylist but was not matched")
	}
}

func TestDenyRules_NotesMdIndexed(t *testing.T) {
	if security.IsDenied("notes.md", security.DefaultDenyPatterns) {
		t.Error("notes.md should NOT be in the denylist but was matched")
	}
}

func TestDenyRules_PrivateKeySkipped(t *testing.T) {
	if !security.IsDenied("private.key", security.DefaultDenyPatterns) {
		t.Error("private.key should be in the denylist (*.key) but was not matched")
	}
}

func TestDenyRules_SSHDirSkipped(t *testing.T) {
	if !security.IsDirDenied("/home/user/.ssh", security.DefaultDenyPatterns) {
		t.Error(".ssh directory should be denied but was not")
	}
}

func TestDenyRules_NodeModulesSkipped(t *testing.T) {
	if !security.IsDirDenied("/project/node_modules", security.DefaultDenyPatterns) {
		t.Error("node_modules directory should be denied but was not")
	}
}

// TestDenyRules_IgnoreFileLoaded verifies that patterns from .laptopaiignore
// are merged with the default patterns and applied correctly.
func TestDenyRules_IgnoreFileLoaded(t *testing.T) {
	dir := t.TempDir()

	// Create the files so IsPathAllowed can resolve them via EvalSymlinks.
	mustWrite(t, filepath.Join(dir, "private-notes.md"), "secret data")
	mustWrite(t, filepath.Join(dir, "normal.md"), "public data")

	// .laptopaiignore blocks private-notes.md
	mustWrite(t, filepath.Join(dir, ".laptopaiignore"), "# custom rules\nprivate-notes.md\n")

	checker, err := security.NewChecker([]string{dir}, dir)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}

	if err := checker.CheckFile(filepath.Join(dir, "private-notes.md")); err == nil {
		t.Error("private-notes.md should be denied by .laptopaiignore, but CheckFile returned nil")
	}

	if err := checker.CheckFile(filepath.Join(dir, "normal.md")); err != nil {
		t.Errorf("normal.md should be allowed, but CheckFile returned: %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
