package vectordb

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const segMagic = "VSEG0001" // 8 bytes

// Segment file layout:
//
//	[8]  "VSEG0001"       magic
//	[8]  created_at       unix nanoseconds, uint64 LE
//	[8]  record_count     uint64 LE
//	--- records sorted ascending by ID ---
//	per record:
//	  [4]  record_length  uint32 LE — byte count of payload below (including CRC)
//	  [8]  id
//	  [8]  created_at
//	  [4]  vector_dim
//	  [dim*4] vector      float32 LE
//	  [4]  norm
//	  [4]  text_length
//	  [?]  text
//	  [4]  metadata_length
//	  [?]  metadata JSON
//	  [4]  CRC32          checksum of id..metadata

// writeSegment sorts records by ID, writes them to a .db file, builds a SegmentIndex
// (every record ID → byte offset), and writes the index to the paired .idx file.
//
// The .db write is atomic via tmp → rename. The .idx write is best-effort — if it fails
// the .db is still valid and the index can be rebuilt by loadSegment on the next Open.
func writeSegment(path string, records []Record) (*SegmentIndex, error) {
	sorted := make([]Record, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("cannot create segment: %w", err)
	}

	bw := bufio.NewWriterSize(f, 256*1024)
	le := binary.LittleEndian

	cleanup := func() { f.Close(); os.Remove(tmp) }

	// Header: 8 + 8 + 8 = 24 bytes
	if _, err := bw.WriteString(segMagic); err != nil {
		cleanup(); return nil, err
	}
	if err := binary.Write(bw, le, uint64(time.Now().UnixNano())); err != nil {
		cleanup(); return nil, err
	}
	if err := binary.Write(bw, le, uint64(len(sorted))); err != nil {
		cleanup(); return nil, err
	}

	offset := int64(24) // tracks current byte position for index building
	idx := newSegmentIndex(len(sorted))

	var recBuf bytes.Buffer
	for _, rec := range sorted {
		recBuf.Reset()
		if err := rec.Encode(&recBuf); err != nil {
			cleanup(); return nil, fmt.Errorf("cannot encode record %d: %w", rec.ID, err)
		}

		// Record the offset before writing so the index points to the length prefix.
		idx.set(rec.ID, offset)

		if _, err := bw.Write(recBuf.Bytes()); err != nil {
			cleanup(); return nil, fmt.Errorf("cannot write record %d: %w", rec.ID, err)
		}
		offset += int64(recBuf.Len())
	}

	if err := bw.Flush(); err != nil {
		cleanup(); return nil, err
	}
	if err := f.Sync(); err != nil {
		cleanup(); return nil, err
	}
	f.Close()

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("cannot finalize segment: %w", err)
	}

	// Write the paired .idx file. Non-fatal if it fails — loadSegment rebuilds it.
	if err := writeIndex(indexPath(path), idx); err != nil {
		// Log but don't fail — the .db is already safely written.
		_ = err
	}

	return idx, nil
}

// loadSegment reads all records from a .db segment file and returns the paired SegmentIndex.
//
// Index loading strategy:
//  1. Try loading the .idx file (fast — small binary file).
//  2. If absent or corrupt, rebuild the index from the .db file while reading records.
//  3. Persist the rebuilt index so the next startup is fast.
func loadSegment(path string) ([]Record, *SegmentIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 256*1024)
	le := binary.LittleEndian

	// Read header
	magic := make([]byte, len(segMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, nil, fmt.Errorf("cannot read magic in %s: %w", filepath.Base(path), err)
	}
	if string(magic) != segMagic {
		return nil, nil, fmt.Errorf("invalid segment %s: wrong magic bytes", filepath.Base(path))
	}
	var createdAt, count uint64
	if err := binary.Read(r, le, &createdAt); err != nil {
		return nil, nil, fmt.Errorf("cannot read created_at in %s: %w", filepath.Base(path), err)
	}
	if err := binary.Read(r, le, &count); err != nil {
		return nil, nil, fmt.Errorf("cannot read count in %s: %w", filepath.Base(path), err)
	}

	// Attempt to load the pre-built index
	idx, idxErr := loadIndex(indexPath(path))
	needsRebuild := idxErr != nil || idx.size() == 0

	// Read all records, tracking byte offsets to rebuild the index if needed
	offset := int64(24) // after the 24-byte header
	records := make([]Record, 0, count)
	rebuildIdx := newSegmentIndex(int(count))

	for i := uint64(0); i < count; i++ {
		var recLen uint32
		if err := binary.Read(r, le, &recLen); err != nil {
			if err == io.EOF {
				break
			}
			return records, idx, fmt.Errorf("%s truncated at record %d: %w", filepath.Base(path), i, err)
		}

		payload := make([]byte, recLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return records, idx, fmt.Errorf("%s payload truncated at record %d: %w", filepath.Base(path), i, err)
		}

		rec, err := decodePayload(payload)
		if err != nil {
			return records, idx, fmt.Errorf("%s record %d corrupt: %w", filepath.Base(path), i, err)
		}

		// offset points to the start of this record (the 4-byte length prefix).
		rebuildIdx.set(rec.ID, offset)
		offset += 4 + int64(recLen)

		records = append(records, rec)
	}

	if needsRebuild {
		idx = rebuildIdx
		// Persist it so next startup doesn't have to rebuild.
		_ = writeIndex(indexPath(path), idx)
	}

	return records, idx, nil
}

// loadAllSegments reads every *.db file in dir and returns all records
// plus a map from segment filename → SegmentIndex for point lookups.
func loadAllSegments(dir string) ([]Record, map[string]*SegmentIndex, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read segments directory: %w", err)
	}

	allRecords := make([]Record, 0)
	allIndexes := make(map[string]*SegmentIndex)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		recs, idx, err := loadSegment(path)
		if err != nil {
			return nil, nil, err
		}
		allRecords = append(allRecords, recs...)
		allIndexes[entry.Name()] = idx
	}
	return allRecords, allIndexes, nil
}
