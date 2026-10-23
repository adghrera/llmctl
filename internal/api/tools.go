package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"llmctl/internal/agent"
	"llmctl/internal/toolexec"
	"llmctl/internal/toolstore"
)

// Installed tool registry (F033): list/install/enable/disable/uninstall and
// test-run for user-installed agent tools. Execution defaults to the
// llm-sandbox framework; per-tool toggle for host execution.

func (s *Server) toolStore() (*toolstore.Store, error) {
	if s.tools == nil {
		ts, err := toolstore.Open()
		if err != nil {
			return nil, err
		}
		s.tools = ts
	}
	return s.tools, nil
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	ts, err := s.toolStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	list, err := ts.List()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// installTool body: {"url": "https://…/tool.zip"} or {"dir": "C:\\path\\to\\tool"}
// optional "id" to pin the registry id.
func (s *Server) installTool(w http.ResponseWriter, r *http.Request) {
	ts, err := s.toolStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		URL string `json:"url"`
		Dir string `json:"dir"`
		ID  string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.Dir = strings.TrimSpace(req.Dir)
	if (req.URL == "") == (req.Dir == "") {
		writeErr(w, 400, "provide exactly one of {url} or {dir}")
		return
	}
	var rec *toolstore.Record
	if req.URL != "" {
		rec, err = ts.InstallFromURL(req.URL, req.ID)
	} else {
		rec, err = ts.InstallFromLocalDir(req.Dir, req.ID)
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) setToolEnabled(w http.ResponseWriter, r *http.Request) {
	ts, err := s.toolStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeErr(w, 400, `expected {"enabled": true|false}`)
		return
	}
	if err := ts.SetEnabled(r.PathValue("id"), *req.Enabled); err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) setToolSandbox(w http.ResponseWriter, r *http.Request) {
	ts, err := s.toolStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Sandbox *bool `json:"sandbox"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Sandbox == nil {
		writeErr(w, 400, `expected {"sandbox": true|false}`)
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	if *req.Sandbox && rec.Manifest.Runtime != "python" {
		writeErr(w, 400, "sandbox supports python tools only")
		return
	}
	if err := ts.SetSandbox(rec.ID, *req.Sandbox); err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) deleteTool(w http.ResponseWriter, r *http.Request) {
	ts, err := s.toolStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// runTool is a synchronous test-run: body {"args": {...}, "timeoutSec": 60}.
func (s *Server) runTool(w http.ResponseWriter, r *http.Request) {
	ts, err := s.toolStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	var req struct {
		Args       map[string]any `json:"args"`
		TimeoutSec int            `json:"timeoutSec"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	}
	argsJSON, _ := json.Marshal(req.Args)
	timeout := time.Duration(req.TimeoutSec) * time.Second
	res := toolexec.Run(r.Context(), rec, string(argsJSON), timeout)
	code := 200
	if !res.OK {
		code = 409
	}
	writeJSON(w, code, res)
}

func statusFor(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid tool id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}

// --- agent integration -------------------------------------------------------

// enabledAgentTools wraps every enabled installed tool as an agent.Tool.
// Names are namespaced "tool:<id>" so they never collide with built-ins.
func (s *Server) enabledAgentTools() map[string]agent.Tool {
	ts, err := s.toolStore()
	if err != nil {
		return nil
	}
	list, err := ts.List()
	if err != nil {
		return nil
	}
	out := map[string]agent.Tool{}
	for i := range list {
		rec := list[i]
		if !rec.Enabled {
			continue
		}
		out["tool:"+rec.ID] = &installedTool{rec: &rec}
	}
	return out
}

type installedTool struct {
	rec *toolstore.Record
}

func (t *installedTool) Name() string { return "tool:" + t.rec.ID }

func (t *installedTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	res := toolexec.Run(ctx, t.rec, argsJSON, 0)
	if !res.OK {
		msg := res.Error
		if msg == "" {
			msg = firstNonEmpty(res.Stderr, "tool failed")
		}
		return "", nil, fmt.Errorf("%s: %s", t.rec.Manifest.Name, msg)
	}
	return res.Result, []map[string]any{{
		"title": t.rec.Manifest.Name,
		"url":   "",
		"snippet": "ran " + map[bool]string{true: "in sandbox", false: "on host"}[res.Sandbox] + " in " + res.Duration,
	}}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// installedToolsPrompt describes enabled installed tools for the system
// prompt so the model knows their names and arguments.
func (s *Server) installedToolsPrompt(extra map[string]agent.Tool) string {
	var b strings.Builder
	b.WriteString("Additional installed tools — call them like the built-ins, e.g.\n")
	ids := make([]string, 0, len(extra))
	for k := range extra {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	for i, k := range ids {
		t := extra[k].(*installedTool)
		params := make([]string, 0, len(t.rec.Manifest.Params))
		for pname, p := range t.rec.Manifest.Params {
			req := ""
			if p.Required {
				req = ", required"
			}
			typ := p.Type
			if typ == "" {
				typ = "string"
			}
			params = append(params, fmt.Sprintf("%s(%s%s): %s", pname, typ, req, p.Description))
		}
		sort.Strings(params)
		mode := "runs on this machine"
		if t.rec.SandboxEnabled() {
			mode = "runs sandboxed"
		}
		b.WriteString(fmt.Sprintf("  <tool_call>{\"name\": %q, \"arguments\": {<params>}}</tool_call> — %s (%s; params: %s)\n",
			k, t.rec.Manifest.Description, mode, strings.Join(params, "; ")))
		_ = i
	}
	b.WriteString("Prefer an installed tool when the user's request matches its description.")
	return b.String()
}
