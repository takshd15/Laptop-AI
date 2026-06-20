package vectordb

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	idxMagic = "VIDX0002" // bumped from 0001; old files are rejected and rebuilt
	idxExt   = ".idx"
)

// indexPath derives the .idx path from the .db segment path.
func indexPath(segPath string) string {
	ext := filepath.Ext(segPath)
	return segPath[:len(segPath)-len(ext)] + idxExt
}

// writeIndex persists a SegmentIndex to a .idx file atomically (tmp → rename).
//
// Wire format:
//
//	[8]  "VIDX0002"       magic
//	[8]  block_count      uint64 LE
//	per block:
//	  [8]  first_id       uint64 LE
//	  [8]  last_id        uint64 LE
//	  [8]  offset         int64  LE
//	  [4]  count          uint32 LE
func writeIndex(path string, idx *SegmentIndex) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("cannot create index tmp file: %w", err)
	}

	w := bufio.NewWriter(f)
	le := binary.LittleEndian

	cleanup := func() { f.Close(); os.Remove(tmp) }

	if _, err := w.WriteString(idxMagic); err != nil {
		cleanup(); return err
	}
	if err := binary.Write(w, le, uint64(len(idx.Blocks))); err != nil {
		cleanup(); return err
	}
	for _, b := range idx.Blocks {
		if err := binary.Write(w, le, b.FirstID); err != nil {
			cleanup(); return err
		}
		if err := binary.Write(w, le, b.LastID); err != nil {
			cleanup(); return err
		}
		if err := binary.Write(w, le, b.Offset); err != nil {
			cleanup(); return err
		}
		if err := binary.Write(w, le, b.Count); err != nil {
			cleanup(); return err
		}
	}
	if err := w.Flush(); err != nil {
		cleanup(); return err
	}
	if err := f.Sync(); err != nil {
		cleanup(); return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// loadIndex reads a .idx file back into a SegmentIndex.
// Returns an empty index (BlockCount==0) if the file does not exist — caller rebuilds.
func loadIndex(path string) (*SegmentIndex, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return newSegmentIndex(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open index %s: %w", filepath.Base(path), err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	le := binary.LittleEndian

	magic := make([]byte, len(idxMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("cannot read index magic in %s: %w", filepath.Base(path), err)
	}
	if string(magic) != idxMagic {
		return nil, fmt.Errorf("invalid index %s: wrong magic (old format — will rebuild)", filepath.Base(path))
	}

	var blockCount uint64
	if err := binary.Read(r, le, &blockCount); err != nil {
		return nil, fmt.Errorf("cannot read block count in %s: %w", filepath.Base(path), err)
	}

	idx := &SegmentIndex{Blocks: make([]BlockIndexEntry, blockCount)}
	for i := uint64(0); i < blockCount; i++ {
		b := &idx.Blocks[i]
		if err := binary.Read(r, le, &b.FirstID); err != nil {
			return nil, fmt.Errorf("index %s block %d first_id: %w", filepath.Base(path), i, err)
		}
		if err := binary.Read(r, le, &b.LastID); err != nil {
			return nil, fmt.Errorf("index %s block %d last_id: %w", filepath.Base(path), i, err)
		}
		if err := binary.Read(r, le, &b.Offset); err != nil {
			return nil, fmt.Errorf("index %s block %d offset: %w", filepath.Base(path), i, err)
		}
		if err := binary.Read(r, le, &b.Count); err != nil {
			return nil, fmt.Errorf("index %s block %d count: %w", filepath.Base(path), i, err)
		}
	}
	return idx, nil
}
