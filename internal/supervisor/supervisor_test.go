package supervisor

import (
	"reflect"
	"testing"
)

func TestRenderLaunch(t *testing.T) {
	args := []string{
		"--port", "{port}",
		"--model", "{model}",
		"--ctx-size", "{var:ctx}",
		"--n-gpu-layers", "{var:ngl}",
		"--parallel", "{var:parallel}",
	}
	vars := map[string]string{"ctx": "2048", "ngl": "0", "parallel": "1"}
	got := renderLaunch(args, `C:\models\qwen.gguf`, vars, 2370)
	want := []string{
		"--port", "2370",
		"--model", `C:\models\qwen.gguf`,
		"--ctx-size", "2048",
		"--n-gpu-layers", "0",
		"--parallel", "1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderLaunch = %v, want %v", got, want)
	}
	// No leftover placeholders.
	for _, a := range got {
		if a == "{port}" || a == "{model}" || a == "{var:ctx}" || a == "{var:ngl}" || a == "{var:parallel}" {
			t.Fatalf("unrendered placeholder left: %q", a)
		}
	}
}

func TestRenderLaunchStdioNoPort(t *testing.T) {
	// stdio path passes port 0; {port} should become "0" (harmless) and
	// model/vars still render.
	args := []string{"--model", "{model}", "--ctx", "{var:ctx}"}
	got := renderLaunch(args, "/m/qwen.gguf", map[string]string{"ctx": "4096"}, 0)
	want := []string{"--model", "/m/qwen.gguf", "--ctx", "4096"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
