// Package gguf parses GGUF file metadata (the KV section of the header) so
// llmctl can show quant, architecture, context length, etc. for installed
// models. Stdlib only; tensor data is never read.
package gguf

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// Value types per the GGUF v3 spec (v2 is a subset).
const (
	tUint8 = iota
	tInt8
	tUint16
	tInt16
	tUint32
	tInt32
	tFloat32
	tBool
	tString
	tArray
	tUint64
	tInt64
	tFloat64
)

// maxArrayKeep caps how many array elements are kept in metadata; larger
// arrays (e.g. tokenizer vocabularies) are summarized instead.
const maxArrayKeep = 64

// File is parsed GGUF metadata.
type File struct {
	Version  uint32         `json:"version"`
	NTensors uint64         `json:"nTensors"`
	Metadata map[string]any `json:"metadata"`
	Summary  Summary        `json:"summary"`
}

// Summary is a curated view of the most useful metadata fields.
type Summary struct {
	Architecture  string `json:"architecture,omitempty"`
	Name          string `json:"name,omitempty"`
	Quant         string `json:"quant,omitempty"`
	ContextLength uint64 `json:"contextLength,omitempty"`
	BlockCount    uint64 `json:"blockCount,omitempty"`
	Tokenizer     string `json:"tokenizer,omitempty"`
}

// quantFileTypes maps llama.cpp's file_type enum to quant labels.
var quantFileTypes = map[uint64]string{
	1: "none", 2: "f32", 3: "f16", 4: "bf16",
	5: "q4_0", 6: "q4_1", 7: "q5_0", 8: "q5_1",
	9: "q2_k", 10: "q3_k_s", 11: "q3_k_m", 12: "q3_k_l",
	13: "q4_k_s", 14: "q4_k_m", 15: "q5_k_s", 16: "q5_k_m",
	17: "q6_k", 18: "q8_k", 19: "q8_0",
}

// Read parses the metadata of a GGUF file at path.
func Read(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	return read(r)
}

// byteReader is what the parser needs: sequential bytes + single-byte reads.
type byteReader interface {
	io.Reader
	io.ByteReader
}

func read(r byteReader) (*File, error) {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("gguf: read magic: %w", err)
	}
	if string(magic) != "GGUF" {
		return nil, fmt.Errorf("gguf: not a GGUF file")
	}
	var out File
	out.Metadata = map[string]any{}
	version, err := readUint32(r)
	if err != nil {
		return nil, err
	}
	out.Version = version
	if version < 2 || version > 3 {
		return nil, fmt.Errorf("gguf: unsupported version %d", version)
	}
	// v2 uses uint32 counts, v3 uint64.
	nTensors, nKV, err := readCounts(r, version)
	if err != nil {
		return nil, err
	}
	out.NTensors = nTensors
	if nKV > 100000 {
		return nil, fmt.Errorf("gguf: implausible kv count %d", nKV)
	}
	for i := uint64(0); i < nKV; i++ {
		key, err := readString(r, version)
		if err != nil {
			return nil, fmt.Errorf("gguf: kv %d key: %w", i, err)
		}
		typ, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		val, err := readValue(r, int(typ), version, 0)
		if err != nil {
			return nil, fmt.Errorf("gguf: kv %q value: %w", key, err)
		}
		out.Metadata[key] = val
	}
	out.Summary = summarize(out.Metadata)
	return &out, nil
}

func summarize(meta map[string]any) Summary {
	var s Summary
	if v, ok := meta["general.architecture"].(string); ok {
		s.Architecture = v
	}
	if v, ok := meta["general.name"].(string); ok {
		s.Name = v
	}
	if v, ok := meta["file_type"]; ok {
		if n := toUint64(v); n > 0 {
			if q, ok := quantFileTypes[n]; ok {
				s.Quant = q
			}
		}
	}
	if v, ok := meta["tokenizer.ggml.model"].(string); ok {
		s.Tokenizer = v
	}
	// Architecture-prefixed keys: <arch>.context_length, <arch>.block_count.
	for k, v := range meta {
		lower := strings.ToLower(k)
		switch {
		case strings.HasSuffix(lower, ".context_length"):
			s.ContextLength = toUint64(v)
		case strings.HasSuffix(lower, ".block_count"):
			s.BlockCount = toUint64(v)
		}
	}
	return s
}

func toUint64(v any) uint64 {
	switch n := v.(type) {
	case uint64:
		return n
	case int64:
		if n > 0 {
			return uint64(n)
		}
	}
	return 0
}

// readValue reads one typed value. depth guards against malformed nesting.
func readValue(r byteReader, typ int, version uint32, depth int) (any, error) {
	if depth > 4 {
		return nil, fmt.Errorf("array nesting too deep")
	}
	switch typ {
	case tBool:
		b, err := r.ReadByte()
		return b != 0, err
	case tUint8:
		b, err := r.ReadByte()
		return uint64(b), err
	case tInt8:
		b, err := r.ReadByte()
		return int64(int8(b)), err
	case tUint16:
		n, err := readUint16(r)
		return uint64(n), err
	case tInt16:
		n, err := readUint16(r)
		return int64(int16(n)), err
	case tUint32:
		n, err := readUint32(r)
		return uint64(n), err
	case tInt32:
		n, err := readUint32(r)
		return int64(int32(n)), err
	case tFloat32:
		n, err := readUint32(r)
		return float64(math.Float32frombits(n)), err
	case tString:
		return readString(r, version)
	case tArray:
		elemType, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		count, err := readCount(r, version)
		if err != nil {
			return nil, err
		}
		if count > 1<<20 {
			return nil, fmt.Errorf("array too large (%d)", count)
		}
		keep := count <= maxArrayKeep
		var items []any
		if keep {
			items = make([]any, 0, count)
		}
		var last any
		for i := uint64(0); i < count; i++ {
			v, err := readValue(r, int(elemType), version, depth+1)
			if err != nil {
				return nil, err
			}
			if keep {
				items = append(items, v)
			} else {
				last = v
			}
		}
		if keep {
			return items, nil
		}
		return fmt.Sprintf("[%d × %s, last=%v]", count, typeName(int(elemType)), last), nil
	case tUint64:
		return readUint64(r)
	case tInt64:
		n, err := readUint64(r)
		return int64(n), err
	case tFloat64:
		n, err := readUint64(r)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(n), nil
	}
	return nil, fmt.Errorf("unknown value type %d", typ)
}

func typeName(t int) string {
	names := []string{"u8", "i8", "u16", "i16", "u32", "i32", "f32", "bool", "str", "array", "u64", "i64", "f64"}
	if t >= 0 && t < len(names) {
		return names[t]
	}
	return "?"
}

func readCount(r byteReader, version uint32) (uint64, error) {
	if version >= 3 {
		return readUint64(r)
	}
	n, err := readUint32(r)
	return uint64(n), err
}

func readCounts(r byteReader, version uint32) (nTensors, nKV uint64, err error) {
	if version >= 3 {
		nTensors, err = readUint64(r)
		if err != nil {
			return
		}
		nKV, err = readUint64(r)
		return
	}
	var a, b uint32
	a, err = readUint32(r)
	if err != nil {
		return
	}
	b, err = readUint32(r)
	return uint64(a), uint64(b), err
}

func readString(r byteReader, version uint32) (string, error) {
	n, err := readCount(r, version)
	if err != nil {
		return "", err
	}
	if n > 1<<26 {
		return "", fmt.Errorf("string too long (%d)", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func readUint16(r byteReader) (uint16, error) {
	lo, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	hi, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16([]byte{lo, hi}), nil
}

func readUint32(r byteReader) (uint32, error) {
	var b [4]byte
	for i := 0; i < 4; i++ {
		v, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		b[i] = v
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

func readUint64(r byteReader) (uint64, error) {
	var b [8]byte
	for i := 0; i < 8; i++ {
		v, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		b[i] = v
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}
