package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// GitHubRelease describes how to fetch a backend from a GitHub repo.
type GitHubRelease struct {
	Repo       string // "owner/name"
	BinaryName string
	Subdir     string
}

// InstallGitHubRelease downloads the newest release asset matching the
// platform, extracts it under destDir/<version>/, and returns the version tag
// and absolute path to the binary. It is manifest-free: callers supply the
// repo and binary name, so built-in and plugin backends both use it.
func InstallGitHubRelease(ctx context.Context, destDir string, gr GitHubRelease, assetHint string, logf func(string)) (version, binPath string, err error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", "", err
	}
	assetURL, version, err := resolveRelease(ctx, gr.Repo, assetHint, logf)
	if err != nil {
		return "", "", err
	}
	archivePath := filepath.Join(destDir, "download.tmp")
	if err := fetch(ctx, assetURL, archivePath, logf); err != nil {
		return "", "", fmt.Errorf("download: %w", err)
	}
	defer os.Remove(archivePath)

	logf("extracting...")
	extractDir := filepath.Join(destDir, version)
	os.RemoveAll(extractDir)
	os.MkdirAll(extractDir, 0o755)
	lower := strings.ToLower(assetURL)
	var format string
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
	default:
		err = os.Rename(archivePath, filepath.Join(extractDir, gr.BinaryName))
	}
	if err != nil {
		os.RemoveAll(extractDir)
		return "", "", fmt.Errorf("extract: %w", err)
	}

	binName := gr.BinaryName
	if runtime.GOOS == "windows" && !strings.HasSuffix(binName, ".exe") {
		binName += ".exe"
	}
	binPath, err = findFile(extractDir, binName, gr.Subdir)
	if err != nil {
		if os.Getenv("LLMCTL_DEBUG_KEEP") == "" {
			os.RemoveAll(extractDir)
		}
		return "", "", fmt.Errorf("binary %q not found in archive: %w (kept at %s)", binName, err, extractDir)
	}
	return version, binPath, nil
}

// resolveRelease queries the GitHub releases API and picks the asset matching
// the platform. It scans the releases list (not /latest) because some repos
// publish stub "latest" releases without binaries (e.g. llama.cpp).
func resolveRelease(ctx context.Context, repo, assetHint string, logf func(string)) (assetURL, version string, err error) {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=15", repo)
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
	candidates := []string{assetHint}
	if assetHint == "" {
		candidates = defaultCandidates()
	}
	osTokens := osTokens()
	for _, rel := range rels {
		for _, cand := range candidates {
			if cand == "" {
				continue
			}
			for _, a := range rel.Assets {
				an := strings.ToLower(a.Name)
				zipLike := strings.HasSuffix(an, ".zip") || strings.HasSuffix(an, ".tar.gz") || strings.HasSuffix(an, ".tgz")
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
		return "", "", fmt.Errorf("no releases found for %s", repo)
	}
	return "", "", fmt.Errorf("no release asset matches %q for %s (assets: %s)",
		assetHint, runtime.GOOS, strings.Join(assetNames(rels[0].Assets), ", "))
}

// defaultCandidates are the fallback asset substrings when no hint is given.
func defaultCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"win-cpu", "cpu", "vulkan"}
	case "darwin":
		return []string{"osx", "arm64", "cpu"}
	default:
		return []string{"linux", "cpu", "vulkan"}
	}
}
