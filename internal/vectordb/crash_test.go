package vectordb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// — file-level helpers ————————————————————————————————————————————————————————
// Build test state via direct file writes so every handle is closed before
// Open() is called. This avoids the Windows file-locking issue that arises
// when a "crashed" DB object still holds a handle to the WAL file.

// makeRec builds a Record with a 3-D vector [v, 0, 0].
func makeRec(id uint64, v float32, text string) Record {
	vec := []float32{v, 0, 0}
	return Record{
		ID:        id,
		Vector:    vec,
		Text:      text,
		Metadata:  map[string]string{"id": fmt.Sprintf("%d", id)},
		Norm:      l2Norm(vec),
		CreatedAt: time.Now().UnixNano(),
	}
}

// writeTestWAL creates dir/wal/wal.log containing the given records,
// properly closed before returning. Simulates a WAL that survived a crash.
func writeTestWAL(t *testing.T, dir string, records []Record) {
	t.Helper()
	walDir := filepath.Join(dir, dirWAL)
	if err := os.MkdirAll(walDir, 0700); err != nil {
		t.Fatalf("mkdir wal: %v", err)
	}
	f, err := os.Create(filepath.Join(walDir, walFileName))
	if err != nil {
		t.Fatalf("create WAL: %v", err)
	}
	if _, err := f.WriteString(walMagic); err != nil {
		f.Close()
		t.Fatalf("write WAL magic: %v", err)
	}
	for _, rec := range records {
		if err := rec.Encode(f); err != nil {
			f.Close()
			t.Fatalf("encode WAL record %d: %v", rec.ID, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close WAL: %v", err)
	}
}

// writeTestSegment writes a properly formatted segment at
// dir/segments/segment_000001.db, closed before returning.
func writeTestSegment(t *testing.T, dir string, records []Record) {
	t.Helper()
	segDir := filepath.Join(dir, dirSegments)
	if err := os.MkdirAll(segDir, 0700); err != nil {
		t.Fatalf("mkdir segments: %v", err)
	}
	path := filepath.Join(segDir, "segment_000001.db")
	if _, err := writeSegment(path, records); err != nil {
		t.Fatalf("writeSegment: %v", err)
	}
}

// ensureSegmentsDir creates the segments directory so Open() finds it.
func ensureSegmentsDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, dirSegments), 0700); err != nil {
		t.Fatalf("mkdir segments: %v", err)
	}
}

// openAndClose opens a DB, runs f with it, then closes it.
// Errors from Close are logged but do not fail the test.
func openAndClose(t *testing.T, dir string, f func(*DB)) {
	t.Helper()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	f(db)
	if err := db.Close(); err != nil {
		t.Logf("Close: %v (non-fatal)", err)
	}
}

// — Crash scenario 1 ————————————————————————————————————————————————————————
// WAL was written; crash happened before the in-memory memtable update.
// On next Open the WAL is replayed and the record is recovered.

func TestCrashRecovery_WALWriteBeforeMemtable(t *testing.T) {
	dir := t.TempDir()
	rec := makeRec(0, 1.0, "crash: WAL only, no memtable")
	writeTestWAL(t, dir, []Record{rec})
	ensureSegmentsDir(t, dir)

	openAndClose(t, dir, func(db *DB) {
		if db.Count() != 1 {
			t.Errorf("after WAL recovery: Count = %d, want 1", db.Count())
		}
		got, ok := db.Get(rec.ID)
		if !ok {
			t.Fatalf("record ID %d not found after WAL recovery", rec.ID)
		}
		if got.Text != rec.Text {
			t.Errorf("recovered Text = %q, want %q", got.Text, rec.Text)
		}
	})
}

// — Crash scenario 2 ————————————————————————————————————————————————————————
// Records in WAL, no segment yet (crash before auto-flush threshold was hit).
// Open replays WAL → all records recovered.

func TestCrashRecovery_MemtableInsertBeforeFlush(t *testing.T) {
	const n = 5
	dir := t.TempDir()

	records := make([]Record, n)
	for i := range records {
		records[i] = makeRec(uint64(i), float32(i), fmt.Sprintf("record %d", i))
	}
	writeTestWAL(t, dir, records)
	ensureSegmentsDir(t, dir)

	openAndClose(t, dir, func(db *DB) {
		if db.Count() != n {
			t.Errorf("after WAL recovery: Count = %d, want %d", db.Count(), n)
		}
	})
}

// — Crash scenario 3 ————————————————————————————————————————————————————————
// Crash during segment write — a .tmp file was left on disk.
// Open must ignore .tmp files and recover from WAL.

func TestCrashRecovery_IncompleteSegmentFile(t *testing.T) {
	const n = 3
	dir := t.TempDir()

	records := make([]Record, n)
	for i := range records {
		records[i] = makeRec(uint64(i), float32(i), fmt.Sprintf("record %d", i))
	}
	writeTestWAL(t, dir, records)

	// Drop a corrupt .tmp file (crash mid-rename: file created, rename not reached).
	segDir := filepath.Join(dir, dirSegments)
	os.MkdirAll(segDir, 0700)
	os.WriteFile(filepath.Join(segDir, "segment_000001.db.tmp"), []byte("CORRUPT TMP DATA"), 0600)

	openAndClose(t, dir, func(db *DB) {
		if db.Count() != n {
			t.Errorf("with .tmp file: Count = %d, want %d", db.Count(), n)
		}
	})
}

// — Crash scenario 4 ————————————————————————————————————————————————————————
// Segment was written but WAL was not yet cleared before the crash.
// Both segment and WAL contain the same records.
// Open must deduplicate via corpusIdx → no duplicate records.

func TestCrashRecovery_SegmentWrittenWALNotCleared(t *testing.T) {
	const n = 5
	dir := t.TempDir()

	records := make([]Record, n)
	for i := range records {
		records[i] = makeRec(uint64(i), float32(i), fmt.Sprintf("record %d", i))
	}

	// Write identical data to both segment and WAL — the crash window.
	writeTestSegment(t, dir, records)
	writeTestWAL(t, dir, records)

	openAndClose(t, dir, func(db *DB) {
		if db.Count() != n {
			t.Errorf("segment+WAL: Count = %d, want %d (expected dedup)", db.Count(), n)
		}
	})
}
