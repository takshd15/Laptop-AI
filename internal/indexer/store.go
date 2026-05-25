package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const storeFile = "index.json"

type Store struct {
	Files map[string]FileRecord `json:"files"`
}

func loadStore(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, storeFile)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{Files: make(map[string]FileRecord)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read index store: %w", err)
	}

	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("cannot parse index store: %w", err)
	}
	if store.Files == nil {
		store.Files = make(map[string]FileRecord)
	}
	return &store, nil
}

func saveStore(dataDir string, store *Store) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	path := filepath.Join(dataDir, storeFile)
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize index store: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// FileCount returns how many files are currently in the store.
func FileCount(dataDir string) (int, error) {
	store, err := loadStore(dataDir)
	if err != nil {
		return 0, err
	}
	return len(store.Files), nil
}

// AllFiles returns every file currently known to the metadata store.
func AllFiles(dataDir string) ([]FileRecord, error) {
	store, err := loadStore(dataDir)
	if err != nil {
		return nil, err
	}
	records := make([]FileRecord, 0, len(store.Files))
	for _, rec := range store.Files {
		records = append(records, rec)
	}
	return records, nil
}
