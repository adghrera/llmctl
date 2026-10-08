package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"llmctl/internal/downloader"
	"llmctl/internal/installer"
	"llmctl/internal/paths"
)

// runPluginInstall executes a plugin's installer spec.
func runPluginInstall(ctx context.Context, m *PluginMeta, assetHint string, logf func(string)) (string, error) {
	ins := m.Install
	dest := paths.BackendDir(m.ID)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	switch ins.Kind {
	case "url":
		return installFromURL(ctx, m, ins, dest, logf)
	case "github-release":
		if ins.Repo == "" {
			return "", fmt.Errorf("github-release install needs repo")
		}
		return installer.Install(ctx, m.ID, ins.Repo, assetHint, logf)
	case "files":
		return installFiles(ctx, m, ins, dest, logf)
	case "":
		return "external", nil
	default:
		return "", fmt.Errorf("unknown install kind %q", ins.Kind)
	}
}

func expandURL(u string) string {
	u = strings.ReplaceAll(u, "{os}", OSExtToken())
	u = strings.ReplaceAll(u, "{arch}", strings.ToLower(runtime.GOARCH))
	u = strings.ReplaceAll(u, "{ext}", strings.TrimPrefix(OSExt(), "."))
	return u
}

func installFromURL(ctx context.Context, m *PluginMeta, ins *PluginInstall, dest string, logf func(string)) (string, error) {
	u := expandURL(ins.URL)
	name := m.Binary
	if name == "" {
		name = filepath.Base(u)
	}
	destFile := filepath.Join(dest, name)
	logf(fmt.Sprintf("downloading %s", u))
	if err := downloader.DownloadFile(ctx, u, destFile, func(p downloader.Progress) {
		if p.Total > 0 {
			logf(fmt.Sprintf("  %d%% (%.1f/%.1f MB)", p.Done*100/p.Total, float64(p.Bytes)/1e6, float64(p.Total)/1e6))
		}
	}); err != nil {
		return "", err
	}
	chmodExec(destFile)
	return "url", nil
}

func installFiles(ctx context.Context, m *PluginMeta, ins *PluginInstall, dest string, logf func(string)) (string, error) {
	for _, f := range ins.Files {
		name := filepath.Base(f.Name)
		u := expandURL(f.URL)
		destFile := filepath.Join(dest, name)
		logf(fmt.Sprintf("downloading %s", u))
		if err := downloader.DownloadFile(ctx, u, destFile, func(p downloader.Progress) {}); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		chmodExec(destFile)
	}
	return "files", nil
}

// chmodExec makes a file executable on unix (no-op on windows).
func chmodExec(p string) {
	if strings.HasSuffix(p, ".exe") {
		return
	}
	_ = os.Chmod(p, 0o755)
}
