package vectordb

const (
	maxMemtableRecords = 10_000
	maxMemtableBytes   = 64 * 1024 * 1024 // 64 MB
)

// memtable holds recently inserted records in RAM before they are flushed to a segment.
// It is the hot path: all writes land here first (after WAL).
type memtable struct {
	records   map[uint64]Record
	sizeBytes int64
}

func newMemtable() *memtable {
	return &memtable{records: make(map[uint64]Record)}
}

func (m *memtable) put(rec Record) {
	if old, exists := m.records[rec.ID]; exists {
		m.sizeBytes -= old.sizeBytes()
	}
	m.records[rec.ID] = rec
	m.sizeBytes += rec.sizeBytes()
}

func (m *memtable) get(id uint64) (Record, bool) {
	rec, ok := m.records[id]
	return rec, ok
}

// all returns every record in the memtable as a slice (order not guaranteed).
func (m *memtable) all() []Record {
	out := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out
}

func (m *memtable) count() int { return len(m.records) }

// shouldFlush returns true when the memtable has grown past either threshold.
// The DB flushes to a segment file when this returns true.
func (m *memtable) shouldFlush() bool {
	return len(m.records) >= maxMemtableRecords || m.sizeBytes >= maxMemtableBytes
}

func (m *memtable) clear() {
	m.records = make(map[uint64]Record)
	m.sizeBytes = 0
}
