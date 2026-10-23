package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"llmctl/internal/agent"
	"llmctl/internal/toolstore"
)

// F033: installed tools join the agent tool map, resolve through the
// "tool:" namespace, and execute via toolexec on the host.
func TestInstalledToolAgentIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LLMCTL_HOME", home)

	// install a tiny python tool directly into the registry dir
	toolsDir := filepath.Join(home, "tools")
	os.MkdirAll(toolsDir, 0o755)
	dir := filepath.Join(toolsDir, "echo-tool-abc123")
	os.MkdirAll(dir, 0o755)
	man := toolstore.Manifest{Name: "Echo", Description: "echoes text", Entry: "main.py", Runtime: "python"}
	// host execution (no container in unit tests)
	off := false
	man.Sandbox = &off
	mb, _ := json.Marshal(man)
	os.WriteFile(filepath.Join(dir, toolstore.ManifestName), mb, 0o644)
	os.WriteFile(filepath.Join(dir, "main.py"),
		[]byte("def run(args):\n    return 'echo:' + args.get('text','')\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".sandbox-off"), []byte("off"), 0o644)

	s := &Server{}
	extra := s.enabledAgentTools()
	tool, ok := extra["tool:echo-tool-abc123"]
	if !ok {
		t.Fatalf("installed tool not in agent map: %v", extra)
	}

	// resolveTool must find it with and without the prefix
	name, resolved, ok := agent.ResolveToolForTest(extra, "echo-tool-abc123")
	if !ok || resolved == nil {
		t.Fatalf("bare name did not resolve: %s", name)
	}
	name, resolved, ok = agent.ResolveToolForTest(extra, "tool:echo-tool-abc123")
	if !ok || resolved == nil {
		t.Fatalf("prefixed name did not resolve: %s", name)
	}

	// execute end-to-end through the agent Tool interface
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	text, _, err := tool.Execute(ctx, `{"text":"hi"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if text != "echo:hi" {
		t.Fatalf("text = %q", text)
	}

	// prompt text mentions the tool and its params
	prompt := s.installedToolsPrompt(extra)
	if !strings.Contains(prompt, "tool:echo-tool-abc123") || !strings.Contains(prompt, "echoes text") {
		t.Fatalf("prompt missing tool info: %s", prompt)
	}

	// disabled tools are excluded
	ts, _ := toolstore.Open()
	ts.SetEnabled("echo-tool-abc123", false)
	if len(s.enabledAgentTools()) != 0 {
		t.Fatal("disabled tool still exposed")
	}
}
