package vectordb

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	idxMagic = "VIDX0001"
	idxExt   = ".idx"
)

// SegmentIndex maps every record ID in a segment to its byte offset in the .db file.
// One index exists per segment, written alongside it as a .idx file.
//
// This lets Get(id) skip the full segment scan and jump directly:
//
//	record 101 → byte 0
//	record 102 → byte 842
//	record 103 → byte 1621
type SegmentIndex struct {
	offsets map[uint64]int64 // record ID → byte offset of that record in the .db file
}

func newSegmentIndex(capacity int) *SegmentIndex {
	return &SegmentIndex{offsets: make(map[uint64]int64, capacity)}
}

func (s *SegmentIndex) set(id uint64, offset int64) {
	s.offsets[id] = offset
}

// Lookup returns the byte offset of id, or -1 if this segment does not contain it.
func (s *SegmentIndex) Lookup(id uint64) int64 {
	if off, ok := s.offsets[id]; ok {
		return off
	}
	return -1
}

func (s *SegmentIndex) size() int { return len(s.offsets) }

// indexPath derives the .idx path from the .db segment path.
func indexPath(segPath string) string {
	ext := filepath.Ext(segPath)
	return segPath[:len(segPath)-len(ext)] + idxExt
}

// writeIndex persists a SegmentIndex to a .idx file atomically (tmp → rename).
//
// Wire format:
//
//	[8]  "VIDX0001"        magic
//	[8]  entry_count       uint64 LE
//	per entry (sorted ascending by ID):
//	  [8]  record_id       uint64 LE
//	  [8]  byte_offset     int64 LE
func writeIndex(path string, idx *SegmentIndex) error {
	type pair struct {
		id  uint64
		off int64
	}
	pairs := make([]pair, 0, len(idx.offsets))
	for id, off := range idx.offsets {
		pairs = append(pairs, pair{id, off})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].id < pairs[j].id })

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
	if err := binary.Write(w, le, uint64(len(pairs))); err != nil {
		cleanup(); return err
	}
	for _, p := range pairs {
		if err := binary.Write(w, le, p.id); err != nil {
			cleanup(); return err
		}
		if err := binary.Write(w, le, p.off); err != nil {
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
// If the file does not exist, it returns an empty index — the caller should rebuild.
func loadIndex(path string) (*SegmentIndex, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return newSegmentIndex(0), nil
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
		return nil, fmt.Errorf("invalid index %s: wrong magic bytes", filepath.Base(path))
	}

	var count uint64
	if err := binary.Read(r, le, &count); err != nil {
		return nil, fmt.Errorf("cannot read entry count in %s: %w", filepath.Base(path), err)
	}

	idx := newSegmentIndex(int(count))
	for i := uint64(0); i < count; i++ {
		var id uint64
		var off int64
		if err := binary.Read(r, le, &id); err != nil {
			return nil, fmt.Errorf("index %s entry %d id: %w", filepath.Base(path), i, err)
		}
		if err := binary.Read(r, le, &off); err != nil {
			return nil, fmt.Errorf("index %s entry %d offset: %w", filepath.Base(path), i, err)
		}
		idx.set(id, off)
	}
	return idx, nil
}
