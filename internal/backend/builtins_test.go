package backend

import (
	"strings"
	"testing"

	"llmctl/internal/store"
)

func TestGufoSpec(t *testing.T) {
	b := &gufoBackend{}
	m := &store.ModelRecord{ID: "qwen", Path: "/models/qwen.gguf"}
	spec, err := b.Spec(m, map[string]string{"ctx": "8192", "servedName": "qwen"})
	if err != nil {
		// gufo binary may not be present; Spec only fails if neither install
		// dir nor PATH has gufo. In a test env that's expected — skip.
		if strings.Contains(err.Error(), "gufo not found") {
			t.Skip("gufo not on PATH in test env")
		}
		t.Fatal(err)
	}
	if spec.HealthPath != "/ready" || spec.APIBasePath != "/v1" {
		t.Errorf("gufo health/api wrong: %s %s", spec.HealthPath, spec.APIBasePath)
	}
	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{"serve", "llm", "--model", "{model}", "--served-model-name", "qwen", "--context", "8192"} {
		if !strings.Contains(joined, want) {
			t.Errorf("gufo args missing %q: %s", want, joined)
		}
	}
}

func TestHalogenSpec(t *testing.T) {
	b := &halogenBackend{}
	spec, err := b.Spec(&store.ModelRecord{ID: "h"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != KindAttach || spec.AttachURL != "http://127.0.0.1:34950" {
		t.Errorf("halogen default: %+v", spec)
	}
	spec2, _ := b.Spec(nil, map[string]string{"url": "http://host:9/"})
	if spec2.AttachURL != "http://host:9" {
		t.Errorf("halogen url override: %s", spec2.AttachURL)
	}
}

func TestContainerSpec(t *testing.T) {
	b := &containerBackend{}
	// Requires image.
	if _, err := b.Spec(nil, nil); err == nil {
		t.Error("container without image should error")
	}
	// Needs a runtime on PATH; skip if none.
	spec, err := b.Spec(&store.ModelRecord{ID: "m", Path: "/models/m.gguf"}, map[string]string{
		"image": "ghcr.io/x/y:1", "cmd": "serve --model {model}", "gpu": "true",
	})
	if err != nil {
		if strings.Contains(err.Error(), "no container runtime") {
			t.Skip("no docker/podman in test env")
		}
		t.Fatal(err)
	}
	if spec.Kind != KindProcess || spec.Transport != TransportHTTP {
		t.Errorf("container kind/transport: %+v", spec)
	}
	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{"run", "--rm", "-p", "{port}:8080", "-v", "{dir}:/models:ro",
		"--device", "/dev/kfd", "ghcr.io/x/y:1", "serve", "--model", "{model}"} {
		if !strings.Contains(joined, want) {
			t.Errorf("container args missing %q: %s", want, joined)
		}
	}
}
