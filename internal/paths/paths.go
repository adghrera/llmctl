// Package paths resolves llmctl's on-disk layout.
//   $LLMCTL_HOME (default ~/.llmctl)
//     state.json          instance registry (atomic writes)
//     llmctl.log          daemon log
//     backends/<id>/      installed backend binaries
//     models/<id>/        downloaded model artifacts
//     logs/<instance>/    per-instance stdout/stderr
package paths

import (
	"os"
	"path/filepath"
)

func Home() string {
	if h := os.Getenv("LLMCTL_HOME"); h != "" {
		abs, _ := filepath.Abs(h)
		return abs
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".llmctl")
}

func StateFile() string  { return filepath.Join(Home(), "state.json") }
func DaemonLog() string  { return filepath.Join(Home(), "llmctl.log") }
func BackendsDir() string { return filepath.Join(Home(), "backends") }
func ModelsDir() string   { return filepath.Join(Home(), "models") }
func LogsDir() string     { return filepath.Join(Home(), "logs") }

func BackendDir(id string) string { return filepath.Join(BackendsDir(), id) }
func ModelDir(id string) string   { return filepath.Join(ModelsDir(), id) }
func InstanceLogDir(id string) string { return filepath.Join(LogsDir(), id) }
func PluginsDir() string          { return filepath.Join(Home(), "plugins") }

// EnsureDirs creates the tree if missing.
func EnsureDirs() error {
	for _, d := range []string{Home(), BackendsDir(), ModelsDir(), LogsDir(), PluginsDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// EnsurePluginsDir creates the plugins dir if missing.
func EnsurePluginsDir() error {
	return os.MkdirAll(PluginsDir(), 0o755)
}
