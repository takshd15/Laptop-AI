package vectordb

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestRecordCodecRoundTrip(t *testing.T) {
	original := Record{
		ID:        42,
		Vector:    []float32{0.1, 0.5, -0.3},
		Text:      "hello world",
		Metadata:  map[string]string{"source": "test.md", "line": "1"},
		Norm:      l2Norm([]float32{0.1, 0.5, -0.3}),
		CreatedAt: 1_700_000_000_000_000_000,
	}

	var buf bytes.Buffer
	if err := original.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := DecodeRecord(&buf)
	if err != nil {
		t.Fatalf("DecodeRecord: %v", err)
	}

	if got.ID != original.ID {
		t.Errorf("ID: got %d, want %d", got.ID, original.ID)
	}
	if got.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt: got %d, want %d", got.CreatedAt, original.CreatedAt)
	}
	if len(got.Vector) != len(original.Vector) {
		t.Fatalf("Vector length: got %d, want %d", len(got.Vector), len(original.Vector))
	}
	for i, v := range original.Vector {
		if got.Vector[i] != v {
			t.Errorf("Vector[%d]: got %f, want %f", i, got.Vector[i], v)
		}
	}
	if got.Text != original.Text {
		t.Errorf("Text: got %q, want %q", got.Text, original.Text)
	}
	if got.Norm != original.Norm {
		t.Errorf("Norm: got %f, want %f", got.Norm, original.Norm)
	}
	for k, v := range original.Metadata {
		if got.Metadata[k] != v {
			t.Errorf("Metadata[%q]: got %q, want %q", k, got.Metadata[k], v)
		}
	}
}

// TestRecordCodecCorrupted_TruncatedPayload encodes a length prefix that claims
// more bytes than are available. DecodeRecord must return an error, not panic.
func TestRecordCodecCorrupted_TruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	// Claim 1000 bytes but provide only 5.
	if err := binary.Write(&buf, binary.LittleEndian, uint32(1000)); err != nil {
		t.Fatalf("binary.Write: %v", err)
	}
	buf.Write([]byte("short"))

	_, err := DecodeRecord(&buf)
	if err == nil {
		t.Fatal("expected error for truncated payload, got nil")
	}
}

// TestRecordCodecCorrupted_BadChecksum encodes a valid record then flips a byte
// in the payload (before the CRC). DecodeRecord must return a checksum error.
func TestRecordCodecCorrupted_BadChecksum(t *testing.T) {
	original := Record{
		ID:        99,
		Vector:    []float32{1.0, 0.0},
		Text:      "checksum test",
		Metadata:  map[string]string{},
		Norm:      1.0,
		CreatedAt: 1000,
	}

	var buf bytes.Buffer
	if err := original.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	// Wire format: [4-byte length][payload…CRC(4 bytes)]
	// Flip a byte in the middle of the payload, well away from the CRC.
	if len(data) < 12 {
		t.Fatal("encoded record too short for corruption test")
	}
	midpoint := 4 + (len(data)-8)/2
	data[midpoint] ^= 0xFF

	_, err := DecodeRecord(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected checksum error, got: %v", err)
	}
}

// TestRecordCodecCorrupted_ShortPayload writes a valid-length prefix but fills
// the payload with garbage too small to contain a valid record.
func TestRecordCodecCorrupted_ShortPayload(t *testing.T) {
	var buf bytes.Buffer
	garbage := []byte{0x01, 0x02, 0x03, 0x04, 0x05} // 5 bytes — far too small
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(garbage))); err != nil {
		t.Fatalf("binary.Write: %v", err)
	}
	buf.Write(garbage)

	_, err := DecodeRecord(&buf)
	if err == nil {
		t.Fatal("expected error for garbage short payload, got nil")
	}
}
