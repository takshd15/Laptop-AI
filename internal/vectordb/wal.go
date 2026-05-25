package vectordb

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	walFileName = "wal.log"
	walMagic    = "VWAL0001" // 8 bytes, identifies a valid WAL file
)

// wal is an append-only write-ahead log using the same binary record format as segments.
//
// Every Insert writes to the WAL before touching the memtable.
// This guarantees that any record visible to the caller survives a crash:
//   - crash before WAL write  → record was never acknowledged, safe to lose
//   - crash after WAL write   → record is replayed on next Open()
//   - crash after segment flush → WAL is truncated, segment has the data
type wal struct {
	f *os.File
	w *bufio.Writer
}

func openWAL(dir string) (*wal, error) {
	path := filepath.Join(dir, walFileName)

	isNew := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		isNew = true
	}

	// O_APPEND is intentionally omitted: on Windows, FILE_APPEND_DATA access
	// prevents Truncate(0). We seek to end manually after replay instead.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot open WAL: %w", err)
	}

	w := &wal{f: f, w: bufio.NewWriterSize(f, 64*1024)}

	if isNew {
		if _, err := f.WriteString(walMagic); err != nil {
			f.Close()
			return nil, fmt.Errorf("cannot write WAL magic: %w", err)
		}
	}

	return w, nil
}

// append encodes rec and appends it atomically (flush after every write).
// Returns only after the OS has accepted the bytes — a crash after this point
// means the record will be recovered on replay.
func (w *wal) append(rec Record) error {
	if err := rec.Encode(w.w); err != nil {
		return fmt.Errorf("WAL encode: %w", err)
	}
	return w.w.Flush()
}

// replay reads all valid records from the WAL and returns them.
// It stops silently at a truncated entry (crash mid-write) — partial writes
// are safe to discard since the Insert was never acknowledged to the caller.
func (w *wal) replay() ([]Record, error) {
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReader(w.f)

	magic := make([]byte, len(walMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		if err == io.EOF {
			return nil, nil // empty WAL file
		}
		return nil, fmt.Errorf("cannot read WAL magic: %w", err)
	}
	if string(magic) != walMagic {
		return nil, fmt.Errorf("invalid WAL file: wrong magic bytes")
	}

	var records []Record
	for {
		rec, err := DecodeRecord(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Truncated or corrupt entry — stop here.
			// Everything before this point was fully written and is valid.
			break
		}
		records = append(records, rec)
	}

	// Reposition to end so subsequent append() calls write after existing data.
	if _, err := w.f.Seek(0, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("WAL seek-to-end after replay: %w", err)
	}
	return records, nil
}

// truncate clears the WAL after a successful segment flush.
// The magic header is rewritten so the file remains valid for future appends.
func (w *wal) truncate() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	if err := w.f.Truncate(0); err != nil {
		return fmt.Errorf("WAL truncate: %w", err)
	}
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.f.WriteString(walMagic); err != nil {
		return fmt.Errorf("cannot rewrite WAL magic after truncate: %w", err)
	}
	if err := w.f.Sync(); err != nil {
		return err
	}
	w.w.Reset(w.f)
	return nil
}

func (w *wal) close() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	return w.f.Close()
}
