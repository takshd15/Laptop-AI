package security

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDenyPatterns are hardcoded and always active regardless of user config.
// These cover the most dangerous file types — credentials, keys, and secret stores.
var DefaultDenyPatterns = []string{
	// Credential files
	".env", ".env.*", ".env.local", ".env.production", ".env.development",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore",
	"id_rsa", "id_ed25519", "id_dsa", "id_ecdsa", "*.pub",
	"credentials", "credentials.*",

	// Secret/password files
	"passwords.*", "password.*", "secrets.*", "secret.*",
	"config/secrets.*", "*.secret",

	// Sensitive document patterns
	"bank*", "tax*", "medical*", "insurance*", "salary*",

	// Version control and tooling internals
	".git/", ".svn/", ".hg/",

	// Dependency trees (large, no useful content for personal search)
	"node_modules/", "venv/", ".venv/", "__pycache__/",
	".mypy_cache/", ".pytest_cache/",

	// SSH directory
	".ssh/",

	// macOS system files
	".DS_Store",
}

// IsDenied reports whether path matches any deny pattern.
// For file patterns, the basename is matched. For directory patterns (ending in /),
// every path component is checked.
func IsDenied(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchDenyPattern(pattern, path) {
			return true
		}
	}
	return false
}

// IsDirDenied reports whether a directory path matches a directory deny pattern.
// Use this during filepath.Walk to skip entire subtrees.
func IsDirDenied(dirPath string, patterns []string) bool {
	base := filepath.Base(dirPath)
	for _, pattern := range patterns {
		if !strings.HasSuffix(pattern, "/") {
			continue
		}
		dirName := strings.TrimSuffix(pattern, "/")
		if matched, _ := filepath.Match(dirName, base); matched {
			return true
		}
	}
	return false
}

// LoadIgnoreFile reads a .laptopaiignore file from folder and returns its patterns.
// Lines starting with # are comments. Empty lines are ignored.
// Returns an empty slice if the file does not exist.
func LoadIgnoreFile(folder string) ([]string, error) {
	path := filepath.Join(folder, ".laptopaiignore")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// matchDenyPattern checks a single deny pattern against a file path.
//
// Pattern types:
//   - Ends with /  → directory match (any path component equals the dir name)
//   - Otherwise    → filename match using filepath.Match (* and ? wildcards)
func matchDenyPattern(pattern, path string) bool {
	if strings.HasSuffix(pattern, "/") {
		// Directory pattern: match any component of the path
		dirName := strings.TrimSuffix(pattern, "/")
		parts := strings.Split(filepath.ToSlash(path), "/")
		for _, part := range parts {
			if matched, _ := filepath.Match(dirName, part); matched {
				return true
			}
		}
		return false
	}

	// File pattern: match against the base name
	base := filepath.Base(path)
	if matched, _ := filepath.Match(pattern, base); matched {
		return true
	}

	// Also try matching the slash-normalised full path (handles "config/secrets.*")
	slash := filepath.ToSlash(path)
	if matched, _ := filepath.Match("*/"+pattern, slash); matched {
		return true
	}
	return false
}
