package rag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EmbedConfig points a knowledge base at an OpenAI-compatible embeddings
// endpoint. When set, chunks are embedded via this endpoint and search ranks by
// embedding cosine similarity instead of local TF-IDF.
type EmbedConfig struct {
	URL    string `json:"url"`   // full embeddings endpoint, e.g. http://host:port/v1/embeddings
	Model  string `json:"model"` // embedding model name
	APIKey string `json:"apiKey,omitempty"`
}

func (c *EmbedConfig) valid() bool {
	return c != nil && c.URL != "" && c.Model != ""
}

// Embed sends texts to the configured endpoint and returns one vector per text,
// in order.
func (c *EmbedConfig) Embed(texts []string) ([][]float64, error) {
	if !c.valid() {
		return nil, fmt.Errorf("embedding config incomplete (need url + model)")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{"model": c.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embeddings %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embeddings decode: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float64, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
