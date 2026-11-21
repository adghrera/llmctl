package gguf

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildGGUF writes a minimal GGUF v3 file with the given KV pairs (only the
// types used in tests) plus fake tensor counts.
func buildGGUF(t *testing.T, kvs map[string]any) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("GGUF")
	binary.Write(&b, binary.LittleEndian, uint32(3)) // version
	binary.Write(&b, binary.LittleEndian, uint64(2)) // n_tensors
	binary.Write(&b, binary.LittleEndian, uint64(len(kvs)))
	writeStr := func(s string) {
		binary.Write(&b, binary.LittleEndian, uint64(len(s)))
		b.WriteString(s)
	}
	for k, v := range kvs {
		writeStr(k)
		switch val := v.(type) {
		case string:
			binary.Write(&b, binary.LittleEndian, uint32(tString))
			writeStr(val)
		case uint32:
			binary.Write(&b, binary.LittleEndian, uint32(tUint32))
			binary.Write(&b, binary.LittleEndian, val)
		case int32:
			binary.Write(&b, binary.LittleEndian, uint32(tInt32))
			binary.Write(&b, binary.LittleEndian, val)
		case float32:
			binary.Write(&b, binary.LittleEndian, uint32(tFloat32))
			binary.Write(&b, binary.LittleEndian, math.Float32bits(val))
		case bool:
			binary.Write(&b, binary.LittleEndian, uint32(tBool))
			if val {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
		case []string:
			binary.Write(&b, binary.LittleEndian, uint32(tArray))
			binary.Write(&b, binary.LittleEndian, uint32(tString))
			binary.Write(&b, binary.LittleEndian, uint64(len(val)))
			for _, s := range val {
				writeStr(s)
			}
		default:
			t.Fatalf("unsupported test type %T", v)
		}
	}
	return b.Bytes()
}

func TestReadGGUF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.gguf")
	data := buildGGUF(t, map[string]any{
		"general.architecture":         "qwen2",
		"general.name":                 "Qwen2 Test",
		"file_type":                    uint32(14), // q4_k_m
		"general.quantization_version": uint32(2),
		"tokenizer.ggml.model":         "gpt2",
		"tokenizer.ggml.add_bos_token": true,
		"qwen2.context_length":         uint32(32768),
		"qwen2.block_count":            uint32(28),
		"tokenizer.ggml.scores":        []string{"a", "b", "c"},
	})
	if err := writeFile(path, data); err != nil {
		t.Fatal(err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 3 || f.NTensors != 2 {
		t.Errorf("header: %+v", f)
	}
	if f.Summary.Architecture != "qwen2" || f.Summary.Name != "Qwen2 Test" {
		t.Errorf("summary arch/name: %+v", f.Summary)
	}
	if f.Summary.Quant != "q4_k_m" {
		t.Errorf("quant: got %q want q4_k_m", f.Summary.Quant)
	}
	if f.Summary.ContextLength != 32768 || f.Summary.BlockCount != 28 {
		t.Errorf("ctx/blocks: %+v", f.Summary)
	}
	if f.Summary.Tokenizer != "gpt2" {
		t.Errorf("tokenizer: %q", f.Summary.Tokenizer)
	}
	if v, ok := f.Metadata["tokenizer.ggml.add_bos_token"].(bool); !ok || !v {
		t.Errorf("bool kv: %v", f.Metadata["tokenizer.ggml.add_bos_token"])
	}
	arr, ok := f.Metadata["tokenizer.ggml.scores"].([]any)
	if !ok || len(arr) != 3 || arr[0] != "a" {
		t.Errorf("string array: %v", f.Metadata["tokenizer.ggml.scores"])
	}
}

func TestReadNotGGUF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.gguf")
	if err := writeFile(path, []byte("NOTGGUFDATA")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Error("expected error for non-GGUF file")
	}
}

func TestLargeArraySummarized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.gguf")
	big := make([]string, maxArrayKeep+10)
	for i := range big {
		big[i] = "tok"
	}
	data := buildGGUF(t, map[string]any{"tokenizer.ggml.tokens": big})
	if err := writeFile(path, data); err != nil {
		t.Fatal(err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := f.Metadata["tokenizer.ggml.tokens"].(string)
	if !ok || v == "" {
		t.Errorf("large array should be summarized string, got %T", f.Metadata["tokenizer.ggml.tokens"])
	}
}

func writeFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}
