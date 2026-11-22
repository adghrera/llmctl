package api

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"llmctl/internal/disk"
	"llmctl/internal/paths"
	"llmctl/internal/store"
)

// doctorCheck is one diagnostic result (F146).
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "warn" | "error"
	Detail string `json:"detail"`
}

// doctor handles GET /api/v1/doctor — a quick health sweep of the daemon,
// its state dir, backends, models, instances, gateway and backups. The CLI
// (llmctl doctor) renders it; the payload's ok flag is false when any check
// errored (F146).
func (s *Server) doctor(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Load()
	if err != nil {
		writeErr(w, 500, "load state: "+err.Error())
		return
	}
	checks := []doctorCheck{{Name: "daemon", Status: "ok", Detail: "llmctl daemon reachable"}}
	add := func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	// State dir writable + free space.
	home := paths.Home()
	probe := filepath.Join(home, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		add("state", "error", "state dir not writable: "+err.Error())
	} else {
		os.Remove(probe)
		if sp, ferr := disk.Free(home); ferr == nil {
			add("state", "ok", fmt.Sprintf("%s, %.1f GiB free", home, float64(sp.FreeBytes)/1e9))
		} else {
			add("state", "ok", home)
		}
	}

	// Installed backends: binary present on disk (PATH backends skipped).
	if len(st.Backends) == 0 {
		add("backends", "warn", "no backends installed")
	} else {
		var missing []string
		for id, b := range st.Backends {
			if b.BinaryPath == "" {
				continue // external backend on PATH
			}
			if _, err := os.Stat(b.BinaryPath); err != nil {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			add("backends", "error", fmt.Sprintf("%d installed, missing binaries: %v", len(st.Backends), missing))
		} else {
			add("backends", "ok", fmt.Sprintf("%d installed", len(st.Backends)))
		}
	}

	// Model files still on disk.
	var gone []string
	for id, m := range st.Models {
		if _, err := os.Stat(m.Path); err != nil {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		add("models", "warn", fmt.Sprintf("%d registered, missing files: %v", len(st.Models), gone))
	} else {
		add("models", "ok", fmt.Sprintf("%d registered, all files present", len(st.Models)))
	}

	// Running instances answer their models endpoint.
	httpClient := &http.Client{Timeout: 3 * time.Second}
	var running, dead []string
	for id, inst := range st.Instances {
		if inst.Status != store.StatusRunning {
			continue
		}
		running = append(running, id)
		if inst.Transport == "stdio" {
			continue // IPC session; status is the best signal
		}
		url := fmt.Sprintf("http://127.0.0.1:%d%s/models", inst.Port, inst.APIBasePath)
		resp, err := httpClient.Get(url)
		if err != nil {
			dead = append(dead, fmt.Sprintf("%s (%v)", id, err))
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			dead = append(dead, fmt.Sprintf("%s (HTTP %d)", id, resp.StatusCode))
		}
	}
	switch {
	case len(dead) > 0:
		add("instances", "error", fmt.Sprintf("unreachable: %v", dead))
	case len(running) > 0:
		add("instances", "ok", fmt.Sprintf("%d running, all reachable", len(running)))
	default:
		add("instances", "ok", "none running")
	}

	// Gateway responds (401 counts as up-but-locked).
	if s.gw != nil {
		gwURL := fmt.Sprintf("http://127.0.0.1:%d", s.gwPort)
		resp, err := httpClient.Get(gwURL + "/v1/models")
		switch {
		case err != nil:
			add("gateway", "error", "no response: "+err.Error())
		case resp.StatusCode == http.StatusUnauthorized:
			resp.Body.Close()
			add("gateway", "ok", "responding (auth required)")
		case resp.StatusCode >= 400:
			resp.Body.Close()
			add("gateway", "warn", fmt.Sprintf("HTTP %d", resp.StatusCode))
		default:
			resp.Body.Close()
			add("gateway", "ok", "responding on /v1/models")
		}
	}

	// Security: gateway exposure (F183). Auth off is fine on loopback, but a
	// public bind with no auth lets anyone on the network call the gateway.
	if s.gw != nil {
		host := s.gw.Addr()
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.TrimSpace(host))
		loopbackOnly := host == "127.0.0.1" || host == "::1" || host == "localhost"
		switch {
		case st.Settings.RequireAuth:
			add("security", "ok", "gateway requires API keys")
		case loopbackOnly:
			add("security", "ok", "no auth, but bound to loopback only ("+s.gw.Addr()+")")
		default:
			add("security", "warn", "no auth and bound to "+s.gw.Addr()+" — enable Require API Key or bind to 127.0.0.1")
		}
	}

	// Backups: newest snapshot name.
	matches, _ := filepath.Glob(filepath.Join(paths.BackupsDir(), "llmctl-*.tar.gz"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	if len(matches) > 0 {
		add("backups", "ok", fmt.Sprintf("%d snapshots, newest %s", len(matches), filepath.Base(matches[0])))
	} else {
		add("backups", "warn", "no snapshots yet")
	}

	ok := true
	for _, c := range checks {
		if c.Status == "error" {
			ok = false
		}
	}
	writeJSON(w, 200, map[string]any{"ok": ok, "checks": checks})
}
