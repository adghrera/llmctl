package mcp

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Setenv("LLMCTL_HOME", t.TempDir())
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	sv, err := st.Create(Server{Name: "fake", Command: "python3", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if sv.ID == "" || !sv.Enabled {
		t.Fatalf("created = %+v", sv)
	}
	if _, err := st.Create(Server{Name: "", Command: "x"}); err == nil {
		t.Error("expected error for empty name")
	}
	if _, err := st.SetEnabled(sv.ID, false); err != nil {
		t.Fatal(err)
	}
	// Reload from disk.
	st2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	list := st2.List()
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("reloaded = %+v", list)
	}
	if err := st2.Delete(sv.ID); err != nil {
		t.Fatal(err)
	}
	if len(st2.List()) != 0 {
		t.Fatal("expected empty after delete")
	}
}

func hasPython() bool {
	_, err := exec.LookPath("python3")
	return err == nil
}

func fakeServer() Server {
	_, thisFile, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(thisFile), "testdata", "fake_mcp.py")
	return Server{ID: "fake", Name: "fake", Command: "python3", Args: []string{script}, Enabled: true}
}

func TestClientHandshakeAndCall(t *testing.T) {
	if !hasPython() {
		t.Skip("python3 not available")
	}
	ctx := context.Background()
	c, err := Connect(ctx, fakeServer())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if len(c.Tools()) != 2 {
		t.Fatalf("tools = %+v", c.Tools())
	}
	got, err := c.CallTool(ctx, "echo", `{"msg":"hello mcp"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello mcp\n" {
		t.Fatalf("call result = %q", got)
	}
	// Tool-level error surfaces as an error.
	if _, err := c.CallTool(ctx, "boom", `{}`); err == nil {
		t.Error("expected error from boom tool")
	}
	// Unknown method surfaces.
	if _, err := c.rpc(ctx, "no/such", nil); err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestClientBadCommand(t *testing.T) {
	ctx := context.Background()
	if _, err := Connect(ctx, Server{ID: "x", Command: "definitely-not-a-real-binary-xyz"}); err == nil {
		t.Error("expected error for bad command")
	}
}

func TestRegistryCaches(t *testing.T) {
	if !hasPython() {
		t.Skip("python3 not available")
	}
	t.Setenv("LLMCTL_HOME", t.TempDir())
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(fakeServer()); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(st)
	defer reg.CloseAll()
	ctx := context.Background()
	c1, err := reg.Client(ctx, "fake")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := reg.Client(ctx, "fake")
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Error("registry did not cache connection")
	}
	reg.Invalidate("fake")
	c3, err := reg.Client(ctx, "fake")
	if err != nil {
		t.Fatal(err)
	}
	if c3 == c1 {
		t.Error("invalidate did not drop connection")
	}
}
