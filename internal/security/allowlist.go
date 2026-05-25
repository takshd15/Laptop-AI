package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsPathAllowed returns true if path is genuinely inside one of the allowed folders.
//
// Two attacks are defended against:
//  1. Path traversal: ~/Notes/../../etc/passwd — filepath.Clean collapses it.
//  2. Symlink escape: ~/Notes/secrets -> /etc/passwd — EvalSymlinks resolves the
//     real target and we re-check that the real path is still within the folder.
//
// If a path cannot be resolved (file doesn't exist yet, broken symlink),
// we deny it — fail closed, not open.
func IsPathAllowed(path string, allowedFolders []string) bool {
	realPath, err := safeResolve(path)
	if err != nil {
		return false
	}

	for _, folder := range allowedFolders {
		realFolder, err := safeResolve(expandHome(folder))
		if err != nil {
			continue
		}
		if isDescendant(realPath, realFolder) {
			return true
		}
	}
	return false
}

// ValidateFolder checks that a folder is safe to add to the allowlist.
// Returns an error with a human-readable explanation if it isn't.
func ValidateFolder(folder string) error {
	resolved, err := safeResolve(expandHome(folder))
	if err != nil {
		return fmt.Errorf("cannot resolve folder %q: %w", folder, err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("folder does not exist: %s", folder)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is a file, not a folder", folder)
	}

	// Refuse indexing the home directory root — too broad
	home, _ := os.UserHomeDir()
	if resolved == filepath.Clean(home) {
		return fmt.Errorf("refusing to index home directory root — use a specific subfolder")
	}
	return nil
}

// safeResolve cleans and resolves symlinks on a path.
func safeResolve(path string) (string, error) {
	clean := filepath.Clean(path)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	return real, nil
}

// isDescendant returns true if path is inside (or equal to) parent.
func isDescendant(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
