package api

import (
	"net/http"

	"llmctl/internal/gpu"
)

// gpuInfo handles GET /api/v1/gpu — detects local GPUs (nvidia-smi /
// rocm-smi) and returns their VRAM totals and usage.
func (s *Server) gpuInfo(w http.ResponseWriter, r *http.Request) {
	gpus := gpu.Detect()
	if gpus == nil {
		gpus = []gpu.GPU{}
	}
	writeJSON(w, 200, map[string]any{"gpus": gpus})
}
