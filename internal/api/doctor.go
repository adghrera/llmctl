package api

import (
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"llmctl/internal/disk"
	"llmctl/internal/paths"
	"llmctl/internal/scheduler"
	"llmctl/internal/store"
	"llmctl/internal/webhook"
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
// stateSpaceCheck turns the state-dir free-space reading into a doctor
// result; warns below 1 GiB free (F236).
func stateSpaceCheck(home string, sp disk.Space) (string, string) {
	free := float64(sp.FreeBytes) / (1 << 30)
	if sp.FreeBytes < 1<<30 {
		return "warn", fmt.Sprintf("%s, only %.2f GiB free — downloads may fail", home, free)
	}
	return "ok", fmt.Sprintf("%s, %.1f GiB free", home, free)
}

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
			status, detail := stateSpaceCheck(home, sp)
			add("state", status, detail)
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

	// Aliases: warn when an alias points at a model that no longer exists (F228).
	if len(st.Aliases) > 0 {
		var dead []string
		for a, target := range st.Aliases {
			if _, ok := st.Models[target]; !ok {
				dead = append(dead, a+"→"+target)
			}
		}
		sort.Strings(dead)
		if len(dead) > 0 {
			add("aliases", "warn", fmt.Sprintf("%d alias(es) point at unknown models: %v", len(dead), dead))
		} else {
			add("aliases", "ok", fmt.Sprintf("%d alias(es), all targets registered", len(st.Aliases)))
		}
	}

	// Notify webhook: warn when recent deliveries to it are failing (F232).
	if hook := strings.TrimSpace(st.Settings.NotifyWebhook); hook != "" {
		var fails int
		var total int
		var lastErr string
		for _, d := range webhook.History(50) {
			if d.URL != hook {
				continue
			}
			total++
			if !d.OK {
				fails++
				if lastErr == "" { // History is newest-first
					lastErr = d.Error
				}
			}
		}
		switch {
		case total == 0:
			add("webhook", "ok", "notify webhook configured, no deliveries yet")
		case fails == 0:
			add("webhook", "ok", fmt.Sprintf("notify webhook: %d recent deliveries, all ok", total))
		default:
			if len(lastErr) > 80 {
				lastErr = lastErr[:80] + "…"
			}
			add("webhook", "warn", fmt.Sprintf("notify webhook: %d/%d recent deliveries failed (last error: %s)", fails, total, lastErr))
		}
	}

	// Orphaned model files: files under the models dir no record points at
	// (left behind by external deletes). Warn with count + size (F206).
	{
		referenced := map[string]bool{}
		for _, m := range st.Models {
			referenced[filepath.Clean(m.Path)] = true
		}
		var orphans []string
		var orphanBytes int64
		root := paths.ModelsDir()
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil // best-effort: skip unreadable entries
			}
			if referenced[filepath.Clean(p)] {
				return nil
			}
			if info, ierr := d.Info(); ierr == nil {
				orphans = append(orphans, strings.TrimPrefix(p, root+string(os.PathSeparator)))
				orphanBytes += info.Size()
			}
			return nil
		})
		if len(orphans) > 0 {
			add("disk", "warn", fmt.Sprintf("%d orphaned file(s) in models dir (%.1f MiB), not referenced by any model: %v",
				len(orphans), float64(orphanBytes)/1e6, orphans))
		} else {
			add("disk", "ok", "no orphaned files in the models dir")
		}
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
			// Auth on but nothing usable is a dead end for clients (F223).
			live := 0
			for _, k := range st.APIKeys {
				if !k.Disabled && (k.ExpiresAt == nil || time.Now().Before(*k.ExpiresAt)) {
					live++
				}
			}
			switch {
			case len(st.APIKeys) == 0:
				add("security", "warn", "auth required but no API keys exist — create one: llmctl key generate")
			case live == 0:
				add("security", "warn", fmt.Sprintf("auth required but all %d key(s) are disabled or expired", len(st.APIKeys)))
			default:
				add("security", "ok", fmt.Sprintf("gateway requires API keys (%d usable)", live))
			}
		case loopbackOnly:
			add("security", "ok", "no auth, but bound to loopback only ("+s.gw.Addr()+")")
		default:
			add("security", "warn", "no auth and bound to "+s.gw.Addr()+" — enable Require API Key or bind to 127.0.0.1")
		}
	}

	// Backups: newest snapshot name + staleness (F181, F207).
	matches, _ := filepath.Glob(filepath.Join(paths.BackupsDir(), "llmctl-*.tar.gz"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	if len(matches) > 0 {
		detail := fmt.Sprintf("%d snapshots, newest %s", len(matches), filepath.Base(matches[0]))
		status := "ok"
		if info, err := os.Stat(matches[0]); err == nil {
			age := time.Since(info.ModTime())
			if age > 7*24*time.Hour {
				status = "warn"
				detail += fmt.Sprintf(" (%d days old — consider a backup)", int(age.Hours())/24)
			}
		}
		add("backups", status, detail)
	} else {
		add("backups", "warn", "no snapshots yet")
	}

	// Scheduler: schedules pointing at missing models, and recent run failures (F334).
	if ts, err := s.schedStore(); err == nil {
		if recs, err := ts.List(); err == nil {
			known := map[string]bool{}
			for id := range st.Models {
				known[strings.ToLower(id)] = true
			}
			for a := range st.Aliases {
				known[strings.ToLower(a)] = true
			}
			var bad []string
			for _, r := range recs {
				k := r.Spec.KindOrDefault()
				if (k == "chat" || k == "instance.start") && r.Spec.Model != "" &&
					!known[strings.ToLower(r.Spec.Model)] {
					bad = append(bad, r.Spec.Name)
				}
			}
			runs := scheduler.Runs(50)
			fails := 0
			for _, run := range runs {
				if run.Status == "error" {
					fails++
				}
			}
			switch {
			case len(bad) > 0:
				add("scheduler", "warn", fmt.Sprintf("%d schedule(s) reference unknown model(s): %v", len(bad), bad))
			case fails > 0:
				add("scheduler", "warn", fmt.Sprintf("%d schedule(s), %d/%d recent run(s) failed", len(recs), fails, len(runs)))
			default:
				add("scheduler", "ok", fmt.Sprintf("%d schedule(s), recent runs ok", len(recs)))
			}
		}
	}

	ok := true
	for _, c := range checks {
		if c.Status == "error" {
			ok = false
		}
	}
	writeJSON(w, 200, map[string]any{"ok": ok, "checks": checks})
}
