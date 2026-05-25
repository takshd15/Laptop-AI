package security

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Checker is the single entry point for all security decisions in the indexer.
// Create one per indexing session via NewChecker, then call Check* on each file.
type Checker struct {
	allowedFolders []string
	denyPatterns   []string // DefaultDenyPatterns + .laptopaiignore patterns
}

// NewChecker builds a Checker for the given folder.
// It loads DefaultDenyPatterns and any .laptopaiignore file found in folder.
func NewChecker(allowedFolders []string, indexFolder string) (*Checker, error) {
	// Start with the hardcoded defaults
	patterns := make([]string, len(DefaultDenyPatterns))
	copy(patterns, DefaultDenyPatterns)

	// Merge .laptopaiignore from the folder being indexed
	extra, err := LoadIgnoreFile(indexFolder)
	if err != nil {
		return nil, fmt.Errorf("loading .laptopaiignore: %w", err)
	}
	patterns = append(patterns, extra...)

	return &Checker{
		allowedFolders: allowedFolders,
		denyPatterns:   patterns,
	}, nil
}

// CheckFile returns nil if the file is safe to index.
// Returns ErrNotAllowed or ErrDenied otherwise.
func (c *Checker) CheckFile(path string) error {
	if !IsPathAllowed(path, c.allowedFolders) {
		return ErrNotAllowed{Path: path}
	}
	if IsDenied(path, c.denyPatterns) {
		return ErrDenied{Path: path}
	}
	return nil
}

// CheckDir returns filepath.SkipDir if the directory should be skipped entirely.
// Call this from filepath.Walk before recursing into a directory.
func (c *Checker) CheckDir(path string) error {
	if !IsPathAllowed(path, c.allowedFolders) {
		return filepath.SkipDir
	}
	if IsDirDenied(path, c.denyPatterns) {
		return filepath.SkipDir
	}
	return nil
}

// IsNotAllowed returns true if err signals a path that is outside the allowlist.
func IsNotAllowed(err error) bool {
	var e ErrNotAllowed
	return errors.As(err, &e)
}

// IsDeniedErr returns true if err signals a path that matches the denylist.
func IsDeniedErr(err error) bool {
	var e ErrDenied
	return errors.As(err, &e)
}

// ErrNotAllowed is returned when a path is outside all allowed folders.
type ErrNotAllowed struct{ Path string }

func (e ErrNotAllowed) Error() string {
	return fmt.Sprintf("path not in allowlist: %s", e.Path)
}

// ErrDenied is returned when a path matches the denylist.
type ErrDenied struct{ Path string }

func (e ErrDenied) Error() string {
	return fmt.Sprintf("path matches denylist: %s", e.Path)
}
