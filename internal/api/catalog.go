package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// CatalogEntry is one curated, popular GGUF model ready for one-click
// install. Repo/File resolve through the same path as `models install`.
type CatalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Repo        string   `json:"repo"`
	File        string   `json:"file"`
	Backend     string   `json:"backend"`
	SizeGB      string   `json:"sizeGB"` // approximate download size, display only
	Quant       string   `json:"quant"`
	Tags        []string `json:"tags,omitempty"`
}

// curatedCatalog is a small, hand-picked list of widely used GGUF models.
// Sizes are approximate and only used for display.
var curatedCatalog = []CatalogEntry{
	{ID: "qwen2.5-7b-instruct-q4", Name: "Qwen2.5 7B Instruct", Description: "Strong all-rounder; good reasoning and coding for its size.", Repo: "bartowski/Qwen2.5-7B-Instruct-GGUF", File: "Qwen2.5-7B-Instruct-Q4_K_M.gguf", Backend: "llama.cpp", SizeGB: "4.7", Quant: "Q4_K_M", Tags: []string{"chat", "coding", "7b"}},
	{ID: "llama-3.1-8b-instruct-q4", Name: "Llama 3.1 8B Instruct", Description: "Meta's 8B chat model; broad ecosystem support.", Repo: "bartowski/Meta-Llama-3.1-8B-Instruct-GGUF", File: "Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf", Backend: "llama.cpp", SizeGB: "4.9", Quant: "Q4_K_M", Tags: []string{"chat", "8b"}},
	{ID: "phi-3.5-mini-q4", Name: "Phi-3.5 Mini", Description: "Microsoft's compact model; punches above its weight.", Repo: "bartowski/Phi-3.5-mini-instruct-GGUF", File: "Phi-3.5-mini-instruct-Q4_K_M.gguf", Backend: "llama.cpp", SizeGB: "2.2", Quant: "Q4_K_M", Tags: []string{"chat", "small"}},
	{ID: "gemma-2-9b-it-q4", Name: "Gemma 2 9B IT", Description: "Google's open 9B instruct model.", Repo: "bartowski/gemma-2-9b-it-GGUF", File: "gemma-2-9b-it-Q4_K_M.gguf", Backend: "llama.cpp", SizeGB: "5.7", Quant: "Q4_K_M", Tags: []string{"chat", "9b"}},
	{ID: "deepseek-r1-7b-q4", Name: "DeepSeek R1 Distill 7B", Description: "Reasoning-focused distilled model.", Repo: "unsloth/DeepSeek-R1-Distill-Qwen-7B-GGUF", File: "DeepSeek-R1-Distill-Qwen-7B-Q4_K_M.gguf", Backend: "llama.cpp", SizeGB: "4.6", Quant: "Q4_K_M", Tags: []string{"reasoning", "7b"}},
	{ID: "qwen2.5-coder-7b-q4", Name: "Qwen2.5 Coder 7B", Description: "Coding-specialized; strong completions and fill-in-middle.", Repo: "Qwen/Qwen2.5-Coder-7B-Instruct-GGUF", File: "qwen2.5-coder-7b-instruct-q4_k_m.gguf", Backend: "llama.cpp", SizeGB: "4.7", Quant: "Q4_K_M", Tags: []string{"coding", "7b"}},
	{ID: "bge-small-en-q8", Name: "BGE Small EN v1.5", Description: "Fast sentence embeddings (~120 MB).", Repo: "ggml-org/bge-small-en-v1.5-Q8_0-GGUF", File: "bge-small-en-v1.5-q8_0.gguf", Backend: "llama.cpp", SizeGB: "0.12", Quant: "Q8_0", Tags: []string{"embeddings"}},
	{ID: "mistral-7b-instruct-v0.3-q4", Name: "Mistral 7B Instruct v0.3", Description: "Classic efficient 7B chat model.", Repo: "bartowski/Mistral-7B-Instruct-v0.3-GGUF", File: "Mistral-7B-Instruct-v0.3-Q4_K_M.gguf", Backend: "llama.cpp", SizeGB: "4.4", Quant: "Q4_K_M", Tags: []string{"chat", "7b"}},
}

// listCatalog returns the curated model catalog.
func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, curatedCatalog)
}

// installCatalogModel installs a catalog entry by id (one-click install).
func (s *Server) installCatalogModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.ID == "" {
		writeErr(w, 400, "id required (a catalog entry id)")
		return
	}
	var entry *CatalogEntry
	for i := range curatedCatalog {
		if strings.EqualFold(curatedCatalog[i].ID, body.ID) {
			entry = &curatedCatalog[i]
			break
		}
	}
	if entry == nil {
		writeErr(w, 404, "unknown catalog id: "+body.ID)
		return
	}
	// Reuse the normal install path by rewriting the request body.
	inner := installModelReq{ID: entry.ID, Backend: entry.Backend, Repo: entry.Repo, File: entry.File}
	b, _ := json.Marshal(inner)
	r.Body = io.NopCloser(strings.NewReader(string(b)))
	s.installModel(w, r)
}
