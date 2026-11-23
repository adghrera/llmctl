package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"llmctl/internal/agent"
	"llmctl/internal/mcp"
)

// mcpStore lazily opens the MCP server registry.
func (s *Server) mcpStore() (*mcp.Store, error) {
	if s.mcpSt == nil {
		st, err := mcp.Open()
		if err != nil {
			return nil, err
		}
		s.mcpSt = st
		s.mcpReg = mcp.NewRegistry(st)
	}
	return s.mcpSt, nil
}

// ---- HTTP handlers ----

// listMCPServers handles GET /api/v1/mcp.
func (s *Server) listMCPServers(w http.ResponseWriter, r *http.Request) {
	ts, err := s.mcpStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ts.List())
}

// createMCPServer handles POST /api/v1/mcp.
// Body: {"name": "...", "command": "...", "args": [...], "env": {...}}
func (s *Server) createMCPServer(w http.ResponseWriter, r *http.Request) {
	ts, err := s.mcpStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var in mcp.Server
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	in.Enabled = true
	sv, err := ts.Create(in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, sv)
}

// deleteMCPServer handles DELETE /api/v1/mcp/{id}.
func (s *Server) deleteMCPServer(w http.ResponseWriter, r *http.Request) {
	ts, err := s.mcpStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	s.mcpReg.Invalidate(r.PathValue("id"))
	writeJSON(w, 200, map[string]any{"ok": true})
}

// setMCPEnabled handles POST /api/v1/mcp/{id}/enabled.
func (s *Server) setMCPEnabled(w http.ResponseWriter, r *http.Request) {
	ts, err := s.mcpStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	sv, err := ts.SetEnabled(r.PathValue("id"), body.Enabled)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	s.mcpReg.Invalidate(r.PathValue("id"))
	writeJSON(w, 200, sv)
}

// testMCPServer handles POST /api/v1/mcp/{id}/test — connects and returns the
// advertised tools.
func (s *Server) testMCPServer(w http.ResponseWriter, r *http.Request) {
	if _, err := s.mcpStore(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	id := r.PathValue("id")
	s.mcpReg.Invalidate(id) // force a fresh connection
	c, err := s.mcpReg.Client(r.Context(), id)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"tools": c.Tools()})
}

// ---- agent integration ----

// mcpTool adapts one MCP server tool to the agent Tool interface.
type mcpTool struct {
	reg      *mcp.Registry
	serverID string
	def      mcp.ToolDef
}

func (t *mcpTool) Name() string { return "mcp:" + t.serverID + ":" + t.def.Name }

func (t *mcpTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	c, err := t.reg.Client(ctx, t.serverID)
	if err != nil {
		return "", nil, err
	}
	text, err := c.CallTool(ctx, t.def.Name, argsJSON)
	return text, nil, err
}

// mcpAgentTools connects to every enabled MCP server and returns their tools
// (keyed mcp:<serverID>:<tool>) plus a prompt fragment describing them.
// Servers that fail to connect are skipped (logged).
func (s *Server) mcpAgentTools(ctx context.Context, keepServers map[string]bool) (map[string]agent.Tool, string) {
	if s.mcpSt == nil || s.mcpReg == nil {
		return nil, ""
	}
	tools := map[string]agent.Tool{}
	var pb strings.Builder
	for _, sv := range s.mcpSt.List() {
		if !sv.Enabled {
			continue
		}
		if keepServers != nil && !keepServers[sv.ID] { // per-chat MCP toggles (F831)
			continue
		}
		c, err := s.mcpReg.Client(ctx, sv.ID)
		if err != nil {
			s.logger.Warn("mcp: connect failed", "server", sv.ID, "err", err)
			continue
		}
		for _, def := range c.Tools() {
			t := &mcpTool{reg: s.mcpReg, serverID: sv.ID, def: def}
			tools[t.Name()] = t
			fmt.Fprintf(&pb, "- %s: %s", t.Name(), def.Description)
			if props, ok := def.InputSchema["properties"].(map[string]any); ok && len(props) > 0 {
				names := make([]string, 0, len(props))
				for k := range props {
					names = append(names, k)
				}
				sort.Strings(names)
				fmt.Fprintf(&pb, " (arguments: %s)", strings.Join(names, ", "))
			}
			pb.WriteByte('\n')
		}
	}
	if len(tools) == 0 {
		return nil, ""
	}
	header := "MCP server tools — call them like the built-ins, e.g.\n  <tool_call>{\"name\": \"mcp:<server>:<tool>\", \"arguments\": {...}}</tool_call>\n"
	return tools, header + pb.String()
}
