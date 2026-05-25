package vectordb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"time"
)

// Record is a single vector database entry.
type Record struct {
	ID        uint64
	Vector    []float32
	Text      string
	Metadata  map[string]string
	Norm      float32 // pre-computed L2 norm — avoids sqrt on every search
	CreatedAt int64   // unix nanoseconds
}

// SearchResult pairs a Record with its cosine similarity score.
type SearchResult struct {
	Record Record
	Score  float32
}

func newRecord(id uint64, vector []float32, text string, metadata map[string]string) Record {
	return Record{
		ID:        id,
		Vector:    vector,
		Text:      text,
		Metadata:  metadata,
		Norm:      l2Norm(vector),
		CreatedAt: time.Now().UnixNano(),
	}
}

// sizeBytes returns a fast in-memory size estimate used for memtable flush thresholds.
func (r *Record) sizeBytes() int64 {
	return int64(8 + 8 + len(r.Vector)*4 + 4 + len(r.Text) + 64)
}

// Encode writes the record in binary format with a CRC32 checksum.
//
// Wire layout:
//
//	[4]  record_length  — byte count of the payload below (INCLUDING checksum bytes)
//	[8]  id
//	[8]  created_at     — unix nanoseconds
//	[4]  vector_dim
//	[dim*4] vector values  — float32 little-endian
//	[4]  norm
//	[4]  text_length
//	[?]  text bytes
//	[4]  metadata_length
//	[?]  metadata JSON
//	[4]  CRC32          — IEEE checksum of all bytes from id through metadata
func (r *Record) Encode(w io.Writer) error {
	metaJSON, err := json.Marshal(r.Metadata)
	if err != nil {
		return fmt.Errorf("cannot marshal metadata: %w", err)
	}

	// Build the payload in a buffer so we can checksum it before writing.
	var payload bytes.Buffer
	le := binary.LittleEndian

	_ = binary.Write(&payload, le, r.ID)
	_ = binary.Write(&payload, le, uint64(r.CreatedAt))
	_ = binary.Write(&payload, le, uint32(len(r.Vector)))
	_ = binary.Write(&payload, le, r.Vector)
	_ = binary.Write(&payload, le, r.Norm)
	_ = binary.Write(&payload, le, uint32(len(r.Text)))
	payload.WriteString(r.Text)
	_ = binary.Write(&payload, le, uint32(len(metaJSON)))
	payload.Write(metaJSON)

	// Append CRC32 over everything written so far.
	checksum := crc32.ChecksumIEEE(payload.Bytes())
	_ = binary.Write(&payload, le, checksum)

	// Write length-prefixed payload to the output writer.
	if err := binary.Write(w, le, uint32(payload.Len())); err != nil {
		return err
	}
	_, err = w.Write(payload.Bytes())
	return err
}

// DecodeRecord reads one length-prefixed record from r, verifies its checksum,
// and returns the decoded Record. Returns io.EOF when r is exhausted.
func DecodeRecord(r io.Reader) (Record, error) {
	var recLen uint32
	if err := binary.Read(r, binary.LittleEndian, &recLen); err != nil {
		return Record{}, err // io.EOF propagates naturally to the caller
	}

	payload := make([]byte, recLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Record{}, fmt.Errorf("record payload truncated: %w", err)
	}

	return decodePayload(payload)
}

// decodePayload verifies the CRC32 and decodes the raw payload bytes
// (everything after the 4-byte length prefix).
func decodePayload(payload []byte) (Record, error) {
	if len(payload) < 4 {
		return Record{}, fmt.Errorf("record too small (%d bytes)", len(payload))
	}

	le := binary.LittleEndian
	storedCRC := le.Uint32(payload[len(payload)-4:])
	actualCRC := crc32.ChecksumIEEE(payload[:len(payload)-4])
	if storedCRC != actualCRC {
		return Record{}, fmt.Errorf("checksum mismatch (stored=%08x actual=%08x): record is corrupt",
			storedCRC, actualCRC)
	}

	data := payload[:len(payload)-4] // strip checksum

	if len(data) < 24 { // id(8) + created_at(8) + dim(4) + norm(4) minimum
		return Record{}, fmt.Errorf("record header too short (%d bytes)", len(data))
	}

	var rec Record
	rec.ID = le.Uint64(data[0:8])
	rec.CreatedAt = int64(le.Uint64(data[8:16]))
	dim := le.Uint32(data[16:20])
	data = data[20:]

	if int(dim)*4 > len(data) {
		return Record{}, fmt.Errorf("vector data truncated (need %d bytes, have %d)", dim*4, len(data))
	}
	rec.Vector = make([]float32, dim)
	for i := range rec.Vector {
		rec.Vector[i] = math.Float32frombits(le.Uint32(data[i*4 : i*4+4]))
	}
	data = data[dim*4:]

	if len(data) < 8 {
		return Record{}, fmt.Errorf("norm/text-length truncated")
	}
	rec.Norm = math.Float32frombits(le.Uint32(data[0:4]))
	textLen := le.Uint32(data[4:8])
	data = data[8:]

	if int(textLen) > len(data) {
		return Record{}, fmt.Errorf("text truncated (need %d, have %d)", textLen, len(data))
	}
	rec.Text = string(data[:textLen])
	data = data[textLen:]

	if len(data) < 4 {
		return Record{}, fmt.Errorf("metadata length missing")
	}
	metaLen := le.Uint32(data[0:4])
	data = data[4:]

	if int(metaLen) > len(data) {
		return Record{}, fmt.Errorf("metadata truncated (need %d, have %d)", metaLen, len(data))
	}
	if err := json.Unmarshal(data[:metaLen], &rec.Metadata); err != nil {
		return Record{}, fmt.Errorf("cannot unmarshal metadata: %w", err)
	}

	return rec, nil
}
