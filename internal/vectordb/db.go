package vectordb

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	dirWAL      = "wal"
	dirSegments = "segments"
)

// DB is the main vector database.
//
// Storage layout (in memory):
//
//	corpus []Record          — all records, flat, append-only
//	segEnds []int            — corpus[segEnds[i-1]:segEnds[i]] = segment i's records
//	                           corpus[segEnds[last]:]           = current memtable records
//	mem *memtable            — map[uint64]Record for O(1) Get of unflushed records
//	corpusIdx map[uint64]int — ID → position in corpus, for dedup and Get
//	segIndexes               — per-segment .idx file data, for disk-based Get fallback
//
// Write path (Insert):
//  1. WAL.append(rec)       — durable before anything else
//  2. mem.put(rec)          — O(1) map insert
//  3. appendToCorpus(rec)   — grows corpus slice, immediately searchable
//  4. if mem.shouldFlush()  → flush: write segment, mark segEnd, truncate WAL
//
// Search path:
//   partitions() slices corpus at segment boundaries → parallelTopKSearch
//   One goroutine per partition (= one goroutine per segment + one for memtable).
//
// Get path:
//   corpusIdx → O(1) in-memory  (covers all records, flushed or not)
//   Fallback: SegmentIndex.Lookup → seek on disk (future lazy-load path)
//
// Crash safety:
//   writeSegment is atomic (tmp→rename). WAL is truncated only after.
//   On next Open, segments load first; WAL replays and deduplicates via corpusIdx.
type DB struct {
	mu         sync.RWMutex
	dir        string
	wal        *wal
	mem        *memtable
	segIndexes map[string]*SegmentIndex // segment filename → full ID→offset map
	corpus     []Record                 // all records, append-only
	corpusIdx  map[uint64]int           // ID → index in corpus
	segEnds    []int                    // end positions of each segment in corpus
	nextID     uint64
}

// Open loads or creates a vector database rooted at dir.
func Open(dir string) (*DB, error) {
	for _, sub := range []string{dirWAL, dirSegments} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			return nil, fmt.Errorf("cannot create %s directory: %w", sub, err)
		}
	}

	db := &DB{
		dir:        dir,
		mem:        newMemtable(),
		segIndexes: make(map[string]*SegmentIndex),
		corpusIdx:  make(map[uint64]int),
	}

	// 1. Load all segment files — each segment becomes one partition.
	entries, err := os.ReadDir(filepath.Join(dir, dirSegments))
	if err != nil {
		return nil, fmt.Errorf("reading segments directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		path := filepath.Join(dir, dirSegments, entry.Name())
		recs, idx, err := loadSegment(path)
		if err != nil {
			return nil, fmt.Errorf("loading segment %s: %w", entry.Name(), err)
		}
		for _, rec := range recs {
			db.appendToCorpus(rec)
		}
		db.segEnds = append(db.segEnds, len(db.corpus)) // mark segment boundary
		db.segIndexes[entry.Name()] = idx
	}

	// 2. Replay WAL — records written after the last segment flush.
	//    Deduplicate against segments already loaded via corpusIdx.
	w, err := openWAL(filepath.Join(dir, dirWAL))
	if err != nil {
		return nil, err
	}
	db.wal = w

	walRecords, err := w.replay()
	if err != nil {
		return nil, fmt.Errorf("WAL replay: %w", err)
	}
	for _, rec := range walRecords {
		if _, exists := db.corpusIdx[rec.ID]; !exists {
			db.appendToCorpus(rec)
			db.mem.put(rec)
		}
	}

	// 3. Determine next ID.
	for _, rec := range db.corpus {
		if rec.ID >= db.nextID {
			db.nextID = rec.ID + 1
		}
	}

	return db, nil
}

// Insert adds a record. The WAL write completes before this returns.
// If the memtable reaches its threshold, a segment flush is triggered automatically.
func (db *DB) Insert(vector []float32, text string, metadata map[string]string) (uint64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	rec := newRecord(db.nextID, vector, text, metadata)

	if err := db.wal.append(rec); err != nil {
		return 0, fmt.Errorf("WAL write: %w", err)
	}

	db.mem.put(rec)
	db.appendToCorpus(rec)
	db.nextID++

	if db.mem.shouldFlush() {
		if err := db.flush(); err != nil {
			return rec.ID, fmt.Errorf("auto-flush: %w", err)
		}
	}
	return rec.ID, nil
}

// Search returns the topK records most similar to query.
// Splits corpus into segment-aligned partitions and searches them in parallel —
// one goroutine per segment. Results are merged with a final heap pass.
func (db *DB) Search(query []float32, topK int) ([]SearchResult, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if len(query) == 0 {
		return nil, fmt.Errorf("query vector is empty")
	}
	return parallelTopKSearch(db.partitions(), query, topK), nil
}

// Get retrieves one record by ID in O(1) via corpusIdx.
// Falls back to a disk seek through the segment index if the record is not in the corpus
// (future lazy-load path — currently all records are always in the corpus).
func (db *DB) Get(id uint64) (Record, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if pos, ok := db.corpusIdx[id]; ok {
		return db.corpus[pos], true
	}

	// Disk fallback via per-segment index (O(segments) seek operations).
	segDir := filepath.Join(db.dir, dirSegments)
	entries, _ := os.ReadDir(segDir)
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.IsDir() || filepath.Ext(e.Name()) != ".db" {
			continue
		}
		idx, ok := db.segIndexes[e.Name()]
		if !ok {
			continue
		}
		offset := idx.Lookup(id)
		if offset < 0 {
			continue
		}
		rec, err := db.readFromSegment(filepath.Join(segDir, e.Name()), offset)
		if err == nil {
			return rec, true
		}
	}
	return Record{}, false
}

// Flush writes the current memtable to a new segment and truncates the WAL.
// Called automatically on threshold; also called by Close.
func (db *DB) Flush() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.flush()
}

// Count returns the total number of records in the corpus.
func (db *DB) Count() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.corpus)
}

// MemtableCount returns the number of records not yet flushed to a segment.
func (db *DB) MemtableCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.mem.count()
}

// Close flushes and closes all file handles.
// The WAL file is always closed even if flush fails, so the OS handle is
// released and callers (including test TempDir cleanup) can remove the file.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	flushErr := db.flush()
	closeErr := db.wal.close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

// — internals (must be called with mu held) —

// partitions returns the corpus split at segment boundaries.
// corpus[segEnds[i-1]:segEnds[i]] → partition i (one per sealed segment)
// corpus[segEnds[last]:]          → partition for current memtable records
//
// Sub-slices are safe here: the RLock prevents any corpus growth while
// goroutines are reading the partitions.
func (db *DB) partitions() [][]Record {
	parts := make([][]Record, 0, len(db.segEnds)+1)
	prev := 0
	for _, end := range db.segEnds {
		if prev < end {
			parts = append(parts, db.corpus[prev:end])
		}
		prev = end
	}
	// Memtable partition (unflushed records after the last segment boundary)
	if prev < len(db.corpus) {
		parts = append(parts, db.corpus[prev:])
	}
	return parts
}

func (db *DB) appendToCorpus(rec Record) {
	db.corpusIdx[rec.ID] = len(db.corpus)
	db.corpus = append(db.corpus, rec)
}

func (db *DB) flush() error {
	if db.mem.count() == 0 {
		return nil
	}

	segPath := db.nextSegmentPath()
	idx, err := writeSegment(segPath, db.mem.all())
	if err != nil {
		return fmt.Errorf("writing segment: %w", err)
	}

	// Truncate WAL only after the segment is durably on disk.
	// Crash between these two is safe: next Open() deduplicates via corpusIdx.
	if err := db.wal.truncate(); err != nil {
		return fmt.Errorf("truncating WAL: %w", err)
	}

	// Record the new segment boundary and update the segment index map.
	db.segEnds = append(db.segEnds, len(db.corpus))
	db.segIndexes[filepath.Base(segPath)] = idx
	db.mem.clear()
	return nil
}

func (db *DB) nextSegmentPath() string {
	entries, _ := os.ReadDir(filepath.Join(db.dir, dirSegments))
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".db" {
			n++
		}
	}
	return filepath.Join(db.dir, dirSegments, fmt.Sprintf("segment_%06d.db", n+1))
}

func (db *DB) readFromSegment(path string, offset int64) (Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return Record{}, fmt.Errorf("seek to offset %d: %w", offset, err)
	}
	return DecodeRecord(bufio.NewReader(f))
}
