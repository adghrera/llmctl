// Package installer downloads and unpacks backend binaries from GitHub
// releases into ~/.llmctl/backends/<id>/.
package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"llmctl/internal/manifest"
	"llmctl/internal/paths"
	"llmctl/internal/store"
)

type Installer struct {
	store *store.Store
	reg   *manifest.Registry
}

func New(st *store.Store, reg *manifest.Registry) *Installer {
	return &Installer{store: st, reg: reg}
}

// Install fetches the backend binary for backendID. assetHint overrides
// the platform asset match (e.g. "vulkan" to pick a different llama.cpp build).
func (in *Installer) Install(ctx context.Context, backendID, assetHint string, logf func(string)) error {
	bdef, ok := in.reg.Backend(backendID)
	if !ok {
		return fmt.Errorf("unknown backend %q", backendID)
	}
	if bdef.Binary.Kind == "path" {
		return fmt.Errorf("backend %q is attach-only: nothing to install (start it yourself, then llmctl start)", backendID)
	}
	dir := paths.BackendDir(bdef.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var assetURL, version string
	var err error
	switch bdef.Binary.Kind {
	case "github-release":
		assetURL, version, err = resolveReleaseAsset(ctx, bdef, assetHint, logf)
	case "url":
		assetURL, version = bdef.Binary.URL, "custom"
	default:
		return fmt.Errorf("backend %q: unknown binary kind %q", backendID, bdef.Binary.Kind)
	}
	if err != nil {
		return err
	}

	// Download archive.
	archivePath := filepath.Join(dir, "download.tmp")
	if err := fetch(ctx, assetURL, archivePath, logf); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.Remove(archivePath)

	logf("extracting...")
	extractDir := filepath.Join(dir, version)
	os.RemoveAll(extractDir)
	os.MkdirAll(extractDir, 0o755)
	// Extract format follows the asset extension, not the manifest.
	format := bdef.Binary.Extract
	lower := strings.ToLower(assetURL)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		format = "zip"
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		format = "tar.gz"
	}
	switch format {
	case "zip":
		err = unzip(archivePath, extractDir)
	case "tar.gz":
		err = untarGz(archivePath, extractDir)
	case "":
		err = os.Rename(archivePath, filepath.Join(extractDir, bdef.Binary.BinaryName))
	default:
		err = fmt.Errorf("unknown extract format %q", bdef.Binary.Extract)
	}
	if err != nil {
		os.RemoveAll(extractDir)
		return fmt.Errorf("extract: %w", err)
	}

	// Locate the binary inside the extracted tree (releases nest dirs).
	binName := bdef.Binary.BinaryName
	if runtime.GOOS == "windows" && !strings.HasSuffix(binName, ".exe") {
		binName += ".exe"
	}
	binPath, err := findFile(extractDir, binName, bdef.Binary.Subdir)
	if err != nil {
		if os.Getenv("LLMCTL_DEBUG_KEEP") == "" {
			os.RemoveAll(extractDir)
		}
		return fmt.Errorf("binary %q not found in archive: %w (kept at %s)", binName, err, extractDir)
	}
	// Windows needs sibling DLLs (cublas, etc.) to stay next to the exe,
	// so we record the path in place rather than moving the exe out.

	return in.store.Update(func(s *store.State) error {
		s.Backends[bdef.ID] = &store.BackendRecord{
			ID: bdef.ID, Version: version, BinaryPath: binPath, InstalledAt: timeNow(),
		}
		return nil
	})
}

// Uninstall removes a backend's files and state record.
func (in *Installer) Uninstall(backendID string) error {
	if err := os.RemoveAll(paths.BackendDir(backendID)); err != nil {
		return err
	}
	return in.store.Update(func(s *store.State) error { delete(s.Backends, backendID); return nil })
}

// resolveReleaseAsset queries the GitHub releases API and picks the asset
// matching the platform (assetHint refines the substring match). It scans
// the releases list (not /latest) because some repos publish stub "latest"
// releases without binaries (e.g. llama.cpp).
func resolveReleaseAsset(ctx context.Context, bdef *manifest.BackendDef, assetHint string, logf func(string)) (assetURL, version string, err error) {
	listURL := strings.Replace(bdef.Binary.URL, "/releases/latest", "/releases?per_page=15", 1)
	if !strings.Contains(listURL, "releases?") {
		listURL = bdef.Binary.URL // already a list URL or direct
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub API: %s (set GITHUB_TOKEN if rate-limited)", resp.Status)
	}
	var rels []struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return "", "", err
	}
	match := bdef.Binary.AssetMatch[manifest.PlatformKey()]
	if assetHint != "" {
		match = assetHint // hint fully overrides the platform default
	}
	// Platform-specific candidate lists, tried in order (preferred first).
	candidates := platformCandidates(match)
	osTokens := osTokens()
	for _, rel := range rels {
		for _, cand := range candidates {
			for _, a := range rel.Assets {
				an := strings.ToLower(a.Name)
				zipLike := strings.HasSuffix(an, ".zip") || strings.HasSuffix(an, ".tar.gz") || strings.HasSuffix(an, ".tgz")
				// Prefer the main llama-* bundle over helper bundles (cudart-*).
				primary := !strings.HasPrefix(an, "cudart")
				onOurOS := osTokens == nil || containsAny(an, osTokens)
				if primary && onOurOS && zipLike && strings.Contains(an, strings.ToLower(cand)) {
					logf(fmt.Sprintf("release %s: asset %s", rel.TagName, a.Name))
					return a.BrowserDownloadURL, sanitize(rel.TagName), nil
				}
			}
		}
	}
	if len(rels) == 0 {
		return "", "", fmt.Errorf("no releases found for %s", bdef.Binary.URL)
	}
	return "", "", fmt.Errorf("no release asset matches %q for %s (assets: %s)",
		match, runtime.GOOS, strings.Join(assetNames(rels[0].Assets), ", "))
}

// platformCandidates broadens a match string with fallbacks:
// "cublas" -> ["cublas", "vulkan"] (llama.cpp CPU-friendly fallback).
func platformCandidates(match string) []string {
	switch runtime.GOOS {
	case "windows":
		if strings.Contains(match, "cublas") {
			return []string{match, "vulkan", "cpu"}
		}
	case "linux":
		if strings.Contains(match, "cuda") {
			return []string{match, "vulkan", "cpu"}
		}
	}
	return []string{match}
}

func assetNames(as []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func fetch(ctx context.Context, url, dest string, logf func(string)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	last := int64(0)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			last += int64(n)
			if last%(8<<20) < 1<<20 {
				logf(fmt.Sprintf("downloaded %.1f MiB", float64(last)/(1<<20)))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func unzip(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target := filepath.Join(dest, f.Name)
		// zip-slip guard
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func untarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

// findFile walks a tree for name, preferring hits under subdir.
func findFile(root, name, subdir string) (string, error) {
	var found, foundAny string
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !fi.IsDir() && strings.EqualFold(fi.Name(), name) {
			if foundAny == "" {
				foundAny = p
			}
			if subdir == "" || strings.Contains(strings.ToLower(p), strings.ToLower(subdir)) {
				found = p
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found != "" {
		return found, nil
	}
	if foundAny != "" {
		return foundAny, nil
	}
	return "", fmt.Errorf("not found under %s", root)
}

func assetOS() string { return runtime.GOOS }

func osTokens() []string {
	switch assetOS() {
	case "windows":
		return []string{"win", "windows"}
	case "linux":
		return []string{"ubuntu", "linux", "debian"}
	case "darwin":
		return []string{"macos", "darwin", "osx", "mac"}
	}
	return nil
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func sanitize(s string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(s)
}
