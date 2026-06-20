package vectordb

const blockSize = 128 // records per block in the sparse index

// BlockIndexEntry covers up to blockSize consecutive records sorted by ID.
// Only the block start offset is stored — to find a record, seek to Offset
// and scan at most Count records.
type BlockIndexEntry struct {
	FirstID uint64
	LastID  uint64
	Offset  int64  // byte offset of the first record in this block
	Count   uint32 // number of records in this block (≤ blockSize)
}

// SegmentIndex is a sparse block index over a segment file.
// Blocks is sorted by FirstID; binary search locates the candidate block.
type SegmentIndex struct {
	Blocks []BlockIndexEntry
}

func newSegmentIndex() *SegmentIndex {
	return &SegmentIndex{}
}

// addRecord extends the index for one record during a segment write.
// Must be called in ascending ID order; offset is the byte start of this record.
func (s *SegmentIndex) addRecord(id uint64, offset int64) {
	n := len(s.Blocks)
	if n == 0 || s.Blocks[n-1].Count >= blockSize {
		s.Blocks = append(s.Blocks, BlockIndexEntry{
			FirstID: id,
			LastID:  id,
			Offset:  offset,
			Count:   1,
		})
		return
	}
	b := &s.Blocks[n-1]
	b.LastID = id
	b.Count++
}

// LookupBlock returns the block offset and record count for the block that may
// contain id. Returns offset=-1 if id is outside any block in this segment.
func (s *SegmentIndex) LookupBlock(id uint64) (offset int64, count uint32) {
	lo, hi := 0, len(s.Blocks)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		b := s.Blocks[mid]
		switch {
		case id < b.FirstID:
			hi = mid - 1
		case id > b.LastID:
			lo = mid + 1
		default:
			return b.Offset, b.Count
		}
	}
	return -1, 0
}

// BlockCount returns the number of blocks in the index.
func (s *SegmentIndex) BlockCount() int { return len(s.Blocks) }
