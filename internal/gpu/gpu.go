// Package gpu detects local GPUs and their VRAM by shelling out to vendor
// tools (nvidia-smi for NVIDIA, rocm-smi for AMD). Detection is best-effort:
// missing tools simply yield no GPUs.
package gpu

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GPU describes one detected graphics device.
type GPU struct {
	Vendor     string `json:"vendor"` // "nvidia" | "amd"
	Name       string `json:"name"`
	TotalBytes int64  `json:"total_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
}

// Detect returns all GPUs found on this host (possibly empty).
func Detect() []GPU {
	var out []GPU
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		out = append(out, detectNvidia()...)
	}
	if _, err := exec.LookPath("rocm-smi"); err == nil {
		out = append(out, detectAMD()...)
	}
	return out
}

func detectNvidia() []GPU {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total,memory.used", "--format=csv,noheader,nounits")
	b, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseNvidiaCSV(b)
}

func parseNvidiaCSV(b []byte) []GPU {
	var out []GPU
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			continue
		}
		total, err1 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		used, err2 := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, GPU{
			Vendor:     "nvidia",
			Name:       strings.TrimSpace(parts[0]),
			TotalBytes: total * 1024 * 1024, // MiB -> bytes
			UsedBytes:  used * 1024 * 1024,
		})
	}
	return out
}

var (
	amdGPUName = regexp.MustCompile(`GPU\[(\d+)\]\s+: Card Series:\s+(.+)`)
	amdTotal   = regexp.MustCompile(`GPU\[(\d+)\]\s+: VRAM Total Memory \(B\): (\d+)`)
	amdUsed    = regexp.MustCompile(`GPU\[(\d+)\]\s+: VRAM Total Used Memory \(B\): (\d+)`)
)

func detectAMD() []GPU {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rocm-smi", "--showproductname", "--showmeminfo", "vram")
	b, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseAMD(b)
}

func parseAMD(b []byte) []GPU {
	byIdx := map[string]*GPU{}
	order := []string{}
	get := func(idx string) *GPU {
		g, ok := byIdx[idx]
		if !ok {
			g = &GPU{Vendor: "amd"}
			byIdx[idx] = g
			order = append(order, idx)
		}
		return g
	}
	for _, line := range strings.Split(string(b), "\n") {
		if m := amdGPUName.FindStringSubmatch(line); m != nil {
			get(m[1]).Name = strings.TrimSpace(m[2])
		} else if m := amdTotal.FindStringSubmatch(line); m != nil {
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil {
				get(m[1]).TotalBytes = n
			}
		} else if m := amdUsed.FindStringSubmatch(line); m != nil {
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil {
				get(m[1]).UsedBytes = n
			}
		}
	}
	var out []GPU
	for _, idx := range order {
		g := byIdx[idx]
		if g.Name == "" {
			g.Name = "AMD GPU " + idx
		}
		out = append(out, *g)
	}
	return out
}
