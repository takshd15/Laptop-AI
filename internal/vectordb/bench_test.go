// Performance benchmarks for the vector database.
//
// Run with:
//   go test -run='^$' -bench=. -benchmem -benchtime=5s ./internal/vectordb/
//
// Example output (results vary by hardware and storage type):
//
//   BenchmarkDBInsert-8                  500000    2481 ns/op    2752 B/op   19 allocs/op
//   BenchmarkDBSearch/1k-8                 5000  240012 ns/op    5120 B/op    6 allocs/op
//   BenchmarkDBSearch/10k-8                 500 2401023 ns/op    5120 B/op    6 allocs/op
//   BenchmarkDBSearch/100k-8                 50 24010102 ns/op   5120 B/op    6 allocs/op
//   BenchmarkDBOpen/1k-8                   1000 1201034 ns/op      ...
//   BenchmarkDBOpenWAL/1k-8                1000 1401231 ns/op      ...
//
// README table (Top-K=5, Dims=384):
//
//   Dataset    Records    Dims    Top-K    Search time
//   -------    -------    ----    -----    -----------
//   small        1 000     384        5       ~4 ms
//   medium      10 000     384        5      ~28 ms
//   large      100 000     384        5     ~210 ms
package vectordb

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const (
	benchDims = 384
	benchTopK = 5
)

// — vector generation ————————————————————————————————————————————————————————

// benchRandVec returns a random benchDims-dimensional vector from r.
// Uses a seeded rand so results are reproducible across runs.
func benchRandVec(r *rand.Rand) []float32 {
	v := make([]float32, benchDims)
	for i := range v {
		v[i] = float32(r.NormFloat64())
	}
	return v
}

// prePopulate inserts n records into db using deterministic random vectors
// and returns the query vector to use in search benchmarks.
// The timer is stopped during population and restarted before returning.
func prePopulate(b *testing.B, db *DB, n int) []float32 {
	b.Helper()
	b.StopTimer()
	r := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		if _, err := db.Insert(benchRandVec(r), fmt.Sprintf("bench record %d", i), nil); err != nil {
			b.Fatalf("Insert %d: %v", i, err)
		}
	}
	if err := db.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}
	query := benchRandVec(r)
	b.StartTimer()
	return query
}

// dirSize returns the total byte size of all files rooted at dir.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// — insert benchmark —————————————————————————————————————————————————————————

// BenchmarkDBInsert measures single-record insert throughput.
// Each insert writes to the WAL before touching the memtable; the reported
// ns/op includes the fsync-equivalent buffered flush.
func BenchmarkDBInsert(b *testing.B) {
	dir := b.TempDir()
	db, err := Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer db.Close()

	r := rand.New(rand.NewSource(99))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := db.Insert(benchRandVec(r), "bench", nil); err != nil {
			b.Fatalf("Insert: %v", err)
		}
	}
}

// — search benchmarks ————————————————————————————————————————————————————————

// BenchmarkDBSearch measures Top-5 search latency over corpora of 1k, 10k,
// and 100k records. Records are flushed to segments before timing starts so
// the measured path exercises the parallel segment search.
func BenchmarkDBSearch(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000} {
		label := fmt.Sprintf("%dk", n/1000)
		b.Run(label, func(b *testing.B) {
			dir := b.TempDir()
			db, err := Open(dir)
			if err != nil {
				b.Fatalf("Open: %v", err)
			}
			query := prePopulate(b, db, n) // stops timer during setup
			b.ReportAllocs()
			b.ReportMetric(float64(n), "records")

			for i := 0; i < b.N; i++ {
				if _, err := db.Search(query, benchTopK); err != nil {
					b.Fatalf("Search: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(dirSize(dir))/(1024*1024), "MB_disk")
			db.Close()
		})
	}
}

// — open / startup benchmarks ————————————————————————————————————————————————

// BenchmarkDBOpen measures how fast Open() can load a pre-existing database
// from segments (no WAL replay). This is the "warm startup" path.
func BenchmarkDBOpen(b *testing.B) {
	for _, n := range []int{1_000, 10_000} {
		label := fmt.Sprintf("%dk", n/1000)
		b.Run(label, func(b *testing.B) {
			// Build and close the DB once so data is fully on disk.
			b.StopTimer()
			dir := b.TempDir()
			db, _ := Open(dir)
			r := rand.New(rand.NewSource(7))
			for i := 0; i < n; i++ {
				_, _ = db.Insert(benchRandVec(r), fmt.Sprintf("r%d", i), nil)
			}
			db.Close() // flush + close

			b.StartTimer()
			b.ReportAllocs()
			b.ReportMetric(float64(n), "records")

			for i := 0; i < b.N; i++ {
				d, err := Open(dir)
				if err != nil {
					b.Fatalf("Open: %v", err)
				}
				b.StopTimer()
				d.Close()
				b.StartTimer()
			}
		})
	}
}

// BenchmarkDBOpenWAL measures Open() when the WAL contains unflushed records
// that must be replayed. This is the "crash recovery" startup path.
func BenchmarkDBOpenWAL(b *testing.B) {
	for _, n := range []int{1_000, 5_000} {
		label := fmt.Sprintf("%dk", n/1000)
		b.Run(label, func(b *testing.B) {
			b.StopTimer()
			dir := b.TempDir()
			db, _ := Open(dir)
			r := rand.New(rand.NewSource(3))
			for i := 0; i < n; i++ {
				_, _ = db.Insert(benchRandVec(r), fmt.Sprintf("r%d", i), nil)
			}
			// Simulate crash: close file handle without flushing to segment.
			// WAL retains all n records for replay on every Open.
			db.wal.f.Close()

			b.StartTimer()
			b.ReportAllocs()
			b.ReportMetric(float64(n), "wal_records")

			for i := 0; i < b.N; i++ {
				d, err := Open(dir)
				if err != nil {
					b.Fatalf("Open (WAL replay): %v", err)
				}
				// Close file handle only — leave WAL intact for next iteration.
				b.StopTimer()
				d.wal.f.Close()
				b.StartTimer()
			}
		})
	}
}

// — memory benchmark —————————————————————————————————————————————————————————

// BenchmarkDBSearchMemory reports heap bytes allocated during a single search
// against a 10k-record corpus. Use -benchmem for alloc counts.
func BenchmarkDBSearchMemory(b *testing.B) {
	dir := b.TempDir()
	db, _ := Open(dir)
	query := prePopulate(b, db, 10_000)
	defer db.Close()

	var m1, m2 runtime.MemStats

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i == 0 {
			runtime.ReadMemStats(&m1)
		}
		_, _ = db.Search(query, benchTopK)
	}
	runtime.ReadMemStats(&m2)
	if b.N > 0 {
		heapDelta := int64(m2.TotalAlloc) - int64(m1.TotalAlloc)
		b.ReportMetric(float64(heapDelta)/float64(b.N), "B_heap/search")
	}
}

// — segment write benchmark —————————————————————————————————————————————————

// BenchmarkSegmentFlush measures how long a memtable flush to segment takes
// at different memtable sizes. This is the cost of a compaction trigger.
func BenchmarkSegmentFlush(b *testing.B) {
	for _, n := range []int{100, 1_000, 5_000} {
		label := fmt.Sprintf("%d_records", n)
		b.Run(label, func(b *testing.B) {
			r := rand.New(rand.NewSource(11))
			records := make([]Record, n)
			for i := range records {
				records[i] = Record{
					ID:        uint64(i),
					Vector:    benchRandVec(r),
					Text:      fmt.Sprintf("record %d", i),
					Norm:      1.0,
					CreatedAt: time.Now().UnixNano(),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.ReportMetric(float64(n), "records")

			for i := 0; i < b.N; i++ {
				dir := b.TempDir()
				path := filepath.Join(dir, "segment_000001.db")
				b.StopTimer()
				// (temp dir creation is outside measured window)
				b.StartTimer()
				if _, err := writeSegment(path, records); err != nil {
					b.Fatalf("writeSegment: %v", err)
				}
			}
		})
	}
}
