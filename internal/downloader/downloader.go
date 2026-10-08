// Package downloader fetches model artifacts with resume support
// (HTTP Range), progress reporting, and sha256 verification.
package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"llmctl/internal/manifest"
	"llmctl/internal/paths"
)

// Progress is a download status snapshot.
type Progress struct {
	Done        bool
	Bytes       int64
	Total       int64 // 0 if unknown
	SpeedBPS    float64
	Error       string
}

// ProgressFn receives progress ticks.
type ProgressFn func(Progress)

// ResolveHFURL builds the resolve URL for a HF-hosted file.
func ResolveHFURL(repo, revision, filename string) string {
	if revision == "" {
		revision = "main"
	}
	return fmt.Sprintf("https://huggingface.co/%s/resolve/%s/%s",
		repo, revision, strings.TrimPrefix(filename, "/"))
}

// artifactURL returns the direct download URL for a model definition.
func artifactURL(m *manifest.ModelDef) (string, error) {
	switch m.Source.Kind {
	case "huggingface":
		return ResolveHFURL(m.Source.Repo, m.Source.Revision, m.Source.Filename), nil
	case "url":
		if m.Source.Repo != "" { // reuse Repo field for direct URLs
			return m.Source.Repo, nil
		}
		return "", fmt.Errorf("model %q: no url set", m.ID)
	default:
		return "", fmt.Errorf("model %q: unknown source kind %q", m.ID, m.Source.Kind)
	}
}

// Destination returns the local path a model artifact should live at.
func Destination(m *manifest.ModelDef) string {
	name := m.Source.Filename
	if name == "" {
		name = m.ID + ".bin"
	}
	return filepath.Join(paths.ModelDir(m.ID), filepath.Base(name))
}

// Download fetches the model artifact to its destination, resuming partial
// .part files via Range requests. Progress ticks are reported ~2x/sec.
func Download(ctx context.Context, m *manifest.ModelDef, onProgress ProgressFn) (string, error) {
	src, err := artifactURL(m)
	if err != nil {
		return "", err
	}
	dest := Destination(m)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	partPath := dest + ".part"

	var offset int64
	if fi, err := os.Stat(partPath); err == nil {
		offset = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		offset = 0 // server ignored Range: restart from scratch
	case http.StatusPartialContent:
		// resume
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("GET %s: %s: %s", src, resp.Status, firstLine(string(body)))
	}

	total := offset
	if resp.ContentLength > 0 {
		total = offset + resp.ContentLength
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, 1<<20) // 1 MiB chunks: good throughput, low memory
	written := offset
	lastTick := time.Now()
	lastBytes := offset
	// Report initial state.
	if onProgress != nil {
		onProgress(Progress{Bytes: written, Total: total})
	}
	for {
		if ctx.Err() != nil {
			return "", ctx.Err() // .part kept: resumable
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", werr
			}
			written += int64(n)
			if onProgress != nil && time.Since(lastTick) > 500*time.Millisecond {
				el := time.Since(lastTick).Seconds()
				onProgress(Progress{Bytes: written, Total: total,
					SpeedBPS: float64(written-lastBytes) / el})
				lastTick, lastBytes = time.Now(), written
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	f.Close()

	if err := os.Rename(partPath, dest); err != nil {
		if !os.IsNotExist(err) {
			os.Remove(dest) // Windows rename-over-existing
			if err2 := os.Rename(partPath, dest); err2 != nil {
				return "", err2
			}
		} else {
			return "", err
		}
	}
	if onProgress != nil {
		onProgress(Progress{Done: true, Bytes: written, Total: written})
	}
	return dest, nil
}

// Checksum computes sha256 of a file (hex).
func Checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HeadSize returns the remote content length (-1 if unknown).
func HeadSize(rawurl string) int64 {
	req, err := http.NewRequest(http.MethodHead, rawurl, nil)
	if err != nil {
		return -1
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
		return n
	}
	return -1
}

// HFCatalog lists GGUF files in a HF repo via the public tree API, so users
// can install models that are not in the curated manifest.
func HFCatalog(repo, revision string) ([]CatalogEntry, error) {
	if revision == "" {
		revision = "main"
	}
	u := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/%s?recursive=true", url.PathEscape(repo), revision)
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HF API %s: %s", repo, resp.Status)
	}
	var items []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
		OID  string `json:"oid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	var out []CatalogEntry
	for _, it := range items {
		if it.Type == "file" && strings.HasSuffix(strings.ToLower(it.Path), ".gguf") {
			out = append(out, CatalogEntry{Path: it.Path, SizeBytes: it.Size, OID: it.OID})
		}
	}
	return out, nil
}

type CatalogEntry struct {
	Path      string
	SizeBytes int64
	OID       string
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
