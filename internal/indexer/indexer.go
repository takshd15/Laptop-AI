package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takshd15/laptop-ai/internal/audit"
	"github.com/takshd15/laptop-ai/internal/security"
)

var supportedExts = map[string]bool{
	".txt":  true,
	".md":   true,
	".go":   true,
	".py":   true,
	".js":   true,
	".ts":   true,
	".pdf":  true,
	".docx": true,
	".html": true,
	".htm":  true,
}

type FileRecord struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Extension string    `json:"extension"`
	Hash      string    `json:"hash"`
}

type Result struct {
	Indexed        int
	Skipped        int
	SkippedSecret  int
	SkippedDenied  int
	Total          int
	Records        []FileRecord
}

// Run walks folder, applies all security checks, and indexes supported files.
// It uses allowedFolders from config to validate paths, loads .laptopaiignore
// from the target folder, scans file hashes for change detection, and scans
// text content for secrets before indexing.
func Run(folder, dataDir string, allowedFolders []string, auditLog *audit.Logger) (*Result, error) {
	folder = expandHome(folder)

	if _, err := os.Stat(folder); os.IsNotExist(err) {
		return nil, fmt.Errorf("folder not found: %s", folder)
	}

	// Build security checker — loads DefaultDenyPatterns + .laptopaiignore
	checker, err := security.NewChecker(allowedFolders, folder)
	if err != nil {
		return nil, fmt.Errorf("cannot initialise security checker: %w", err)
	}

	limits := security.DefaultLimits()

	store, err := loadStore(dataDir)
	if err != nil {
		return nil, err
	}

	result := &Result{}

	walkErr := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			auditLog.Log(audit.EventError, path, err.Error())
			return nil
		}

		if info.IsDir() {
			// Depth guard — skip entire subtrees that are too deep.
			if depthErr := limits.CheckDepth(folder, path); depthErr != nil {
				auditLog.Log(audit.EventSkippedDepth, path, depthErr.Error())
				return filepath.SkipDir
			}

			// Skip denied or out-of-allowlist directories entirely.
			if skip := checker.CheckDir(path); skip == filepath.SkipDir {
				auditLog.Log(audit.EventSkippedDenied, path, "directory in denylist or outside allowlist")
				return filepath.SkipDir
			}
			return nil
		}

		// Symlink guard — skip symlinked files by default.
		if sym, symErr := security.IsSymlink(path); symErr == nil && sym {
			auditLog.Log(audit.EventSkippedSymlink, path, "symlinks are not followed by default")
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))

		// Binary extension guard — skip before any content read.
		if security.IsBinaryExt(ext) {
			return nil
		}

		if !supportedExts[ext] {
			return nil
		}

		result.Total++

		// File size guard — skip before reading content.
		if sizeErr := limits.CheckFileSize(info); sizeErr != nil {
			auditLog.Log(audit.EventSkippedTooLarge, path, sizeErr.Error())
			fmt.Printf("  skip  %s (%s)\n", path, sizeErr.Error())
			result.Skipped++
			return nil
		}

		// Security check: allowlist + denylist
		if checkErr := checker.CheckFile(path); checkErr != nil {
			if security.IsNotAllowed(checkErr) {
				auditLog.Log(audit.EventSkippedAllowed, path, "outside allowed folders")
				result.SkippedDenied++
			} else {
				auditLog.Log(audit.EventSkippedDenied, path, "matches denylist")
				result.SkippedDenied++
			}
			result.Skipped++
			return nil
		}

		hash, hashErr := hashFile(path)
		if hashErr != nil {
			fmt.Printf("  warn  cannot hash %s\n", path)
			return nil
		}

		// Skip unchanged files
		if existing, ok := store.Files[path]; ok && existing.Hash == hash {
			result.Skipped++
			auditLog.Log(audit.EventSkippedUnchanged, path, "")
			fmt.Printf("  skip  %s\n", path)
			return nil
		}

		// Security check: scan file content for secrets before indexing.
		// Use ReadFile directly here; extractor with timeout is used later during
		// the chunk/embed pipeline when full text is needed.
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Printf("  warn  cannot read %s\n", path)
			return nil
		}
		if matches := security.ScanForSecrets(string(content)); len(matches) > 0 {
			desc := security.DescribeMatches(matches)
			// Log path and pattern type only — never the content or matched value.
			auditLog.Log(audit.EventSkippedSecret, path, "possible secret: "+desc)
			fmt.Printf("  skip  %s (possible secret: %s)\n", path, desc)
			result.SkippedSecret++
			result.Skipped++
			return nil
		}

		rec := FileRecord{
			Path:      path,
			Size:      info.Size(),
			Modified:  info.ModTime(),
			Extension: ext,
			Hash:      hash,
		}

		store.Files[path] = rec
		result.Records = append(result.Records, rec)
		result.Indexed++
		auditLog.Log(audit.EventIndexed, path, "")
		fmt.Printf("  index %s\n", path)
		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("walk failed: %w", walkErr)
	}

	if err := saveStore(dataDir, store); err != nil {
		return nil, err
	}

	return result, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
