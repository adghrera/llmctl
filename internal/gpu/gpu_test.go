package gpu

import "testing"

func TestParseNvidiaCSV(t *testing.T) {
	in := []byte("NVIDIA GeForce RTX 4090, 24564, 1024\nNVIDIA GeForce RTX 3060, 12288, 512\n\n")
	gpus := parseNvidiaCSV(in)
	if len(gpus) != 2 {
		t.Fatalf("got %d GPUs, want 2", len(gpus))
	}
	if gpus[0].Name != "NVIDIA GeForce RTX 4090" || gpus[0].Vendor != "nvidia" {
		t.Errorf("gpu0 = %+v", gpus[0])
	}
	if gpus[0].TotalBytes != 24564*1024*1024 || gpus[0].UsedBytes != 1024*1024*1024 {
		t.Errorf("gpu0 mem = %+v", gpus[0])
	}
}

func TestParseAMD(t *testing.T) {
	// Sampled from real rocm-smi output (warnings on stderr are not captured).
	in := []byte(`
====================================== Product Info ======================================
GPU[0]		: Card Series: 		Radeon 8060S Graphics
GPU[0]		: Card Model: 		0x1586
================================== Memory Usage (Bytes) ==================================
GPU[0]		: VRAM Total Memory (B): 536870912
GPU[0]		: VRAM Total Used Memory (B): 465481728
`)
	gpus := parseAMD(in)
	if len(gpus) != 1 {
		t.Fatalf("got %d GPUs, want 1", len(gpus))
	}
	g := gpus[0]
	if g.Vendor != "amd" || g.Name != "Radeon 8060S Graphics" {
		t.Errorf("gpu = %+v", g)
	}
	if g.TotalBytes != 536870912 || g.UsedBytes != 465481728 {
		t.Errorf("gpu mem = %+v", g)
	}
}

func TestParseAMDMissingName(t *testing.T) {
	in := []byte("GPU[1]\t: VRAM Total Memory (B): 100\n")
	gpus := parseAMD(in)
	if len(gpus) != 1 || gpus[0].Name != "AMD GPU 1" {
		t.Fatalf("gpus = %+v", gpus)
	}
}

func TestParseGarbage(t *testing.T) {
	if len(parseNvidiaCSV([]byte("error: driver not loaded"))) != 0 {
		t.Error("nvidia: expected no GPUs from garbage")
	}
	if len(parseAMD([]byte("no devices found"))) != 0 {
		t.Error("amd: expected no GPUs from garbage")
	}
}
