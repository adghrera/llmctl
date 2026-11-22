// Command llmctl is the CLI for the llmctl daemon. It is a thin client over
// the management API: `llmctl up` runs the daemon (gateway + API + UI on one
// port); every other command talks to the running daemon.
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"llmctl/internal/backup"
	"llmctl/internal/daemon"
	"llmctl/internal/paths"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "up":
		err = cmdUp(args)
	case "down":
		err = cmdDown(args)
	case "status":
		err = cmdStatus(args)
	case "doctor":
		err = cmdDoctor(args)
	case "ps":
		err = cmdPS(args)
	case "backends":
		err = cmdBackends(args)
	case "models":
		err = cmdModels(args)
	case "start":
		err = cmdStart(args)
	case "stop":
		err = cmdStop(args)
	case "restart":
		err = cmdRestart(args)
	case "logs":
		err = cmdLogs(args)
	case "key":
		err = cmdKey(args)
	case "autostart":
		err = cmdAutoStart(args)
	case "requests":
		err = cmdRequests(args)
	case "backups":
		err = cmdBackups(args)
	case "usage":
		err = cmdUsage(args)
	case "metrics":
		err = cmdMetrics(args)
	case "events":
		err = cmdEvents(args)
	case "scheduler":
		err = cmdScheduler(args)
	case "chat":
		err = cmdChat(args)
	case "backup":
		err = cmdBackup(args)
	case "restore":
		err = cmdRestore(args)
	case "version", "--version", "-v":
		fmt.Printf("llmctl %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`llmctl — multi-backend local LLM manager with an OpenAI-compatible gateway

Usage:
  llmctl up [--addr :8080] [--rate 20] [--burst 60] [--max-inflight 512] [--api-key K]
  llmctl down
  llmctl status [-json]
  llmctl doctor
  llmctl ps
  llmctl backends list
  llmctl backends install <id> [--asset <hint>]
  llmctl backends uninstall <id>
  llmctl backends add <plugin.json>
  llmctl backends remove <id>
  llmctl models list
  llmctl models show <id>
  llmctl models install <repo-or-url> [--file f.gguf] [--rev main] [--backend llama.cpp] [--id ID]
  llmctl models remove <id> [--file]
  llmctl models ctx <model-id> <tokens|0>
  llmctl models price <model-id> <in-per-M> <out-per-M>
  llmctl models hf-search <query>
  llmctl models hf-files <repo>
  llmctl start <model> [--backend ID] [--instance NAME] [--var k=v ...]
  llmctl stop <instance> [--force]
  llmctl restart <instance>
  llmctl logs <instance> [--stream stderr|stdout] [--tail N]
  llmctl logs -daemon [--tail N]
  llmctl key generate [name]
  llmctl key list
  llmctl key revoke <id>
  llmctl key reset-usage <id>
  llmctl autostart list
  llmctl autostart add <model> [--backend ID] [--instance NAME] [--port N] [--var k=v ...]
  llmctl autostart remove <model>
  llmctl scheduler list
  llmctl scheduler add --name N --cron "0 9 * * *" [--kind chat|instance.start|instance.stop|backup] [--model ID] [--prompt TEXT] [--instance ID]
  llmctl scheduler run|enable|disable|delete <id>
  llmctl scheduler runs [-n 20]
  llmctl requests [--model ID] [--status 4|404] [--min-ms N] [--key PREFIX] [--instance ID] [--id ID] [-n 20]
  llmctl backups [list|create|restore <name>|delete <name>]
  llmctl usage [-n 14] [-key ID] [-keys] [--csv]
  llmctl metrics
  llmctl events [-n 20]
  llmctl chat [--model ID] [--key KEY]
  llmctl backup [file.tar.gz]     export state (config/chats/KBs/memory/…)
  llmctl restore <file.tar.gz>    import a backup into the state dir
  llmctl version

The daemon serves the gateway (/v1), the management API (/api/v1), and the
web UI (http://127.0.0.1:8080/) on one port.
`)
}

// reorderFlags hoists --flag value pairs before positionals (Go's flag
// package stops parsing at the first positional arg).
func reorderFlags(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") || strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

// ---- commands ----

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	rate := fs.Float64("rate", 20, "sustained req/s per client")
	burst := fs.Int("burst", 60, "burst size per client")
	maxInf := fs.Int("max-inflight", 512, "max concurrent gateway requests")
	apiKey := fs.String("api-key", "", "optional static gateway key")
	fs.Parse(reorderFlags(args))
	opts := daemon.Options{Addr: *addr, RateRPS: *rate, RateBurst: *burst, MaxInflight: *maxInf}
	if *apiKey != "" {
		opts.APIKeys = []string{*apiKey}
	}
	fmt.Println("llmctl daemon starting on", *addr, "(Ctrl-C to stop)")
	fmt.Println("web UI: http://127.0.0.1:8080/")
	return daemon.Run(context.Background(), opts)
}

func cmdDown(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Post("/api/v1/shutdown", nil)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if code != 200 {
		return fmt.Errorf("shutdown returned %d", code)
	}
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw status JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/status")
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Println(string(out))
		_ = code
		return nil
	}
	var st map[string]any
	json.Unmarshal(out, &st)
	ver, _ := st["version"].(string)
	up := ""
	if sec, ok := st["uptimeSec"].(float64); ok && sec > 0 {
		up = ", up " + uptimeShort(time.Duration(sec)*time.Second)
	}
	fmt.Printf("daemon: running (%s) v%s%s\n", cc.Base, ver, up)
	fmt.Printf("backends: %v installed\n", st["backends"])
	fmt.Printf("models: %v downloaded\n", st["models"])
	if run, ok := st["running"]; ok {
		fmt.Printf("instances: %v (%v running)\n", st["instances"], run)
	} else {
		fmt.Printf("instances: %v\n", st["instances"])
	}
	if list, ok := st["runningInstances"].([]any); ok && len(list) > 0 {
		for _, e := range list {
			m, _ := e.(map[string]any)
			id, _ := m["id"].(string)
			model, _ := m["model"].(string)
			sec, _ := m["uptimeSec"].(float64)
			fmt.Printf("  %-16s %-16s up %s\n", id, model, uptimeShort(time.Duration(sec)*time.Second))
		}
	}
	auth := ""
	if v, ok := st["requireAuth"].(bool); ok && v {
		auth = " (auth on)"
	}
	fmt.Printf("gateway: %v%s\n", st["gateway"], auth)
	if reqs, ok := st["todayRequests"].(float64); ok {
		errs, _ := st["todayErrors"].(float64)
		cost, _ := st["todayCost"].(float64)
		fmt.Printf("today: %d reqs, %d errors, $%.4f est. cost\n", int64(reqs), int64(errs), cost)
	}
	_ = code
	return nil
}

// cmdDoctor runs the daemon-side health sweep and prints one line per check
// (F146). Exit code is non-zero when any check errored.
func cmdDoctor(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/doctor")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("doctor returned %d: %s", code, out)
	}
	var res struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	sym := map[string]string{"ok": "[ok]  ", "warn": "[warn]", "error": "[fail]"}
	for _, c := range res.Checks {
		s, ok := sym[c.Status]
		if !ok {
			s = c.Status
		}
		fmt.Printf("%s %-9s %s\n", s, c.Name, c.Detail)
	}
	if !res.OK {
		return fmt.Errorf("doctor: some checks failed")
	}
	return nil
}

func cmdPS(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	_, out, err := cc.Get("/api/v1/instances")
	if err != nil {
		return err
	}
	var list []struct {
		ID        string    `json:"id"`
		ModelID   string    `json:"modelId"`
		BackendID string    `json:"backendId"`
		Transport string    `json:"transport"`
		Port      int       `json:"port"`
		Status    string    `json:"status"`
		StartedAt time.Time `json:"startedAt"`
		Served    int64     `json:"served"`
		Note      string    `json:"note"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no instances (start one: llmctl start <model-id>)")
		return nil
	}
	fmt.Printf("%-28s %-24s %-12s %-8s %-7s %-9s %-8s %6s %s\n", "INSTANCE", "MODEL", "BACKEND", "TRANS", "PORT", "STATUS", "UPTIME", "SERVED", "NOTE")
	for _, i := range list {
		up := ""
		if i.Status == "running" && !i.StartedAt.IsZero() {
			up = uptimeShort(time.Since(i.StartedAt))
		}
		note := i.Note
		if len(note) > 40 {
			note = note[:37] + "..."
		}
		fmt.Printf("%-28s %-24s %-12s %-8s %-7v %-9s %-8s %6d %s\n",
			i.ID, i.ModelID, i.BackendID, i.Transport, i.Port, i.Status, up, i.Served, note)
	}
	return nil
}

// uptimeShort formats a duration compactly: 45s, 12m, 3h05m, 2d04h.
func uptimeShort(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	default:
		return fmt.Sprintf("%dd%02dh", s/86400, (s%86400)/3600)
	}
}

func cmdBackends(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl backends <list|install|uninstall|add|remove>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		_, out, err := cc.Get("/api/v1/backends")
		if err != nil {
			return err
		}
		var list []map[string]any
		json.Unmarshal(out, &list)
		fmt.Printf("%-14s %-8s %-8s %-10s %s\n", "ID", "TRANS", "KIND", "STATUS", "DESCRIPTION")
		for _, b := range list {
			status := "not installed"
			if b["installed"] == true {
				status = "installed " + fmt.Sprint(b["version"])
			}
			fmt.Printf("%-14s %-8s %-8s %-10s %s\n", b["id"], b["transport"], b["kind"], status, b["description"])
		}
		return nil
	case "install":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backends install <id> [--asset hint]")
		}
		id := rest[0]
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		asset := fs.String("asset", "", "asset hint")
		fs.Parse(reorderFlags(rest[1:]))
		path := "/api/v1/backends/" + id + "/install"
		if *asset != "" {
			path += "?asset=" + *asset
		}
		code, out, err := cc.Post(path, nil)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 202 {
			return fmt.Errorf("install returned %d", code)
		}
		fmt.Println("install started (track: llmctl status / web UI)")
		return nil
	case "uninstall":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backends uninstall <id>")
		}
		code, out, err := cc.Post("/api/v1/backends/"+rest[0]+"/uninstall", nil)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("uninstall returned %d", code)
		}
		return nil
	case "add":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backends add <plugin.json>")
		}
		b, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("invalid plugin JSON: %w", err)
		}
		code, out, err := cc.Post("/api/v1/backends", m)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 201 {
			return fmt.Errorf("add returned %d", code)
		}
		return nil
	case "remove":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backends remove <id>")
		}
		code, out, err := cc.Delete("/api/v1/backends/" + rest[0])
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("remove returned %d", code)
		}
		return nil
	default:
		return fmt.Errorf("unknown backends subcommand: %s", sub)
	}
}

func cmdModels(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl models <list|show|install|remove|hf-search|hf-files>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		_, out, err := cc.Get("/api/v1/models")
		if err != nil {
			return err
		}
		var list []struct {
			ID        string  `json:"id"`
			BackendID string  `json:"backendId"`
			SizeBytes int64   `json:"sizeBytes"`
			CtxSize   int     `json:"ctxSize"`
			Running   bool    `json:"running"`
			PriceIn   float64 `json:"priceInPerM"`
			PriceOut  float64 `json:"priceOutPerM"`
			IdleMin   int     `json:"idleUnloadMin"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no models downloaded")
			return nil
		}
		fmt.Printf("%-40s %-12s %-10s %-9s %-14s %-7s %s\n", "ID", "BACKEND", "SIZE", "CTX", "PRICE $/M", "IDLE", "RUNNING")
		for _, m := range list {
			ctxv := "-"
			if m.CtxSize > 0 {
				ctxv = fmt.Sprintf("%d", m.CtxSize)
			}
			price := "free"
			if m.PriceIn > 0 || m.PriceOut > 0 {
				price = fmt.Sprintf("%.2f/%.2f", m.PriceIn, m.PriceOut)
			}
			idle := "-"
			switch {
			case m.IdleMin < 0:
				idle = "pinned"
			case m.IdleMin > 0:
				idle = fmt.Sprintf("%dm", m.IdleMin)
			}
			fmt.Printf("%-40s %-12s %-10v %-9s %-14s %-7s %v\n", m.ID, m.BackendID, m.SizeBytes, ctxv, price, idle, m.Running)
		}
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models show <id>")
		}
		code, out, err := cc.Get("/api/v1/models/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models show returned %d: %s", code, out)
		}
		var mv struct {
			ID            string   `json:"id"`
			BackendID     string   `json:"backendId"`
			Path          string   `json:"path"`
			SizeBytes     int64    `json:"sizeBytes"`
			Running       bool     `json:"running"`
			Instances     []string `json:"instances"`
			CtxSize       int      `json:"ctxSize"`
			PriceIn       float64  `json:"priceInPerM"`
			PriceOut      float64  `json:"priceOutPerM"`
			IdleUnloadMin int      `json:"idleUnloadMin"`
		}
		if err := json.Unmarshal(out, &mv); err != nil {
			return err
		}
		idle := "global"
		switch {
		case mv.IdleUnloadMin < 0:
			idle = "pinned"
		case mv.IdleUnloadMin > 0:
			idle = fmt.Sprintf("%dm", mv.IdleUnloadMin)
		}
		price := "free"
		if mv.PriceIn > 0 || mv.PriceOut > 0 {
			price = fmt.Sprintf("$%.2f/$%.2f per M", mv.PriceIn, mv.PriceOut)
		}
		ctxv := "backend default"
		if mv.CtxSize > 0 {
			ctxv = strconv.Itoa(mv.CtxSize)
		}
		status := "ready"
		if mv.Running {
			status = "running"
		}
		fmt.Printf("id:       %s\nbackend:  %s\npath:     %s\nsize:     %d bytes\nctx:      %s\nprice:    %s\nidle:     %s\nstatus:   %s\n", mv.ID, mv.BackendID, mv.Path, mv.SizeBytes, ctxv, price, idle, status)
		if len(mv.Instances) > 0 {
			fmt.Printf("instances: %s\n", strings.Join(mv.Instances, ", "))
		}
		return nil
	case "install":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models install <repo-or-url> [--file f] [--rev r] [--backend id] [--id ID]")
		}
		repo := rest[0]
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		file := fs.String("file", "", "filename in repo")
		rev := fs.String("rev", "", "HF revision")
		backend := fs.String("backend", "", "backend id (default llama.cpp)")
		id := fs.String("id", "", "model id")
		fs.Parse(reorderFlags(rest[1:]))
		body := map[string]any{"repo": repo, "file": *file, "revision": *rev, "backend": *backend, "id": *id}
		code, out, err := cc.Post("/api/v1/models/install", body)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 202 {
			return fmt.Errorf("install returned %d", code)
		}
		fmt.Println("download started (track: web UI or llmctl status)")
		return nil
	case "remove":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models remove <id> [--file]")
		}
		path := "/api/v1/models/" + rest[0]
		for _, a := range rest[1:] {
			if a == "--file" {
				path += "?file=true" // also delete a registered file outside the models dir (F162)
			}
		}
		code, out, err := cc.Delete(path)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("remove returned %d", code)
		}
		return nil
	case "ctx":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models ctx <model-id> <tokens|0>  (0 = backend default)")
		}
		id := rest[0]
		n := 0
		if len(rest) > 1 {
			v, err := strconv.Atoi(rest[1])
			if err != nil {
				return fmt.Errorf("invalid token count: %s", rest[1])
			}
			n = v
		}
		code, out, err := cc.Do("PUT", "/api/v1/models/"+urlEscape(id)+"/ctx", map[string]any{"ctxSize": n})
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("ctx returned %d", code)
		}
		return nil
	case "price":
		if len(rest) < 3 {
			return fmt.Errorf("usage: llmctl models price <model-id> <in-per-M> <out-per-M>  (USD, 0 = free)")
		}
		inP, err := strconv.ParseFloat(rest[1], 64)
		if err != nil {
			return fmt.Errorf("invalid price: %s", rest[1])
		}
		outP, err := strconv.ParseFloat(rest[2], 64)
		if err != nil {
			return fmt.Errorf("invalid price: %s", rest[2])
		}
		code, out, err := cc.Do("PATCH", "/api/v1/models/"+urlEscape(rest[0])+"/price",
			map[string]any{"priceInPerM": inP, "priceOutPerM": outP})
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("price returned %d", code)
		}
		return nil
	case "hf-search":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models hf-search <query>")
		}
		q := strings.Join(rest, " ")
		_, out, err := cc.Get("/api/v1/hf/search?q=" + urlEscape(q))
		if err != nil {
			return err
		}
		var res []map[string]any
		json.Unmarshal(out, &res)
		for _, m := range res {
			fmt.Println(m["modelId"])
		}
		return nil
	case "hf-files":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models hf-files <repo>")
		}
		_, out, err := cc.Get("/api/v1/hf/files?repo=" + urlEscape(rest[0]))
		if err != nil {
			return err
		}
		var files []map[string]any
		json.Unmarshal(out, &files)
		for _, f := range files {
			fmt.Printf("%s  (%v bytes)\n", f["path"], f["sizeBytes"])
		}
		return nil
	default:
		return fmt.Errorf("unknown models subcommand: %s", sub)
	}
}

func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	backend := fs.String("backend", "", "backend id")
	instance := fs.String("instance", "", "instance name")
	var vars stringList
	fs.Var(&vars, "var", "parameter k=v (repeatable)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl start <model> [--backend id] [--instance name] [--var k=v ...]")
	}
	model := fs.Arg(0)
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	body := map[string]any{"model": model, "backend": *backend, "instance": *instance, "vars": vars.Map()}
	code, out, err := cc.Post("/api/v1/instances", body)
	if err != nil {
		return err
	}
	var inst map[string]any
	json.Unmarshal(out, &inst)
	if code != 200 {
		return fmt.Errorf("start failed: %s", string(out))
	}
	transport := fmt.Sprint(inst["transport"])
	if transport == "stdio" {
		fmt.Printf("instance %q running (stdio, pid %v)\n", inst["id"], inst["pid"])
	} else {
		fmt.Printf("instance %q running on http://127.0.0.1:%v\n", inst["id"], inst["port"])
	}
	fmt.Println("try: curl http://127.0.0.1:8080/v1/chat/completions -d '{\"model\":\"" + model + "\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'")
	return nil
}

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	force := fs.Bool("force", false, "force kill")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl stop <instance> [--force]")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	path := "/api/v1/instances/" + fs.Arg(0)
	if *force {
		path += "?force=true"
	}
	code, out, err := cc.Delete(path)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if code != 200 {
		return fmt.Errorf("stop returned %d", code)
	}
	return nil
}

func cmdRestart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl restart <instance>")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Post("/api/v1/instances/"+args[0]+"/restart", nil)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if code != 200 {
		return fmt.Errorf("restart returned %d", code)
	}
	return nil
}

// cmdLogs prints the tail of an instance's log (stderr by default).
func cmdLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	stream := fs.String("stream", "stderr", "log stream: stderr|stdout")
	tail := fs.Int("tail", 4000, "bytes to show from the end")
	daemonFlag := fs.Bool("daemon", false, "show the daemon's own log instead of an instance")
	fs.Parse(reorderFlags(args))
	if *daemonFlag {
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get(fmt.Sprintf("/api/v1/logs/daemon?bytes=%d", *tail))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("daemon log returned %d: %s", code, out)
		}
		var v struct {
			Logs string `json:"logs"`
		}
		if err := json.Unmarshal(out, &v); err != nil {
			return err
		}
		fmt.Print(v.Logs)
		return nil
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl logs <instance> [--stream stderr|stdout] [--tail N] | llmctl logs -daemon [--tail N]")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/v1/instances/%s/logs?stream=%s&bytes=%d",
		fs.Arg(0), url.QueryEscape(*stream), *tail)
	code, out, err := cc.Get(path)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("logs returned %d: %s", code, out)
	}
	var v struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return err
	}
	fmt.Print(v.Logs)
	return nil
}

// cmdScheduler manages cron-scheduled tasks (list/add/run/enable/disable/delete).
func cmdScheduler(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl scheduler <list|add|run|enable|disable|delete> [id] [flags]")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	printOut := func(code int, out []byte, okCode int) error {
		fmt.Println(string(out))
		if code != okCode {
			return fmt.Errorf("%s returned %d", sub, code)
		}
		return nil
	}
	switch sub {
	case "list":
		code, out, err := cc.Get("/api/v1/scheduler")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler list returned %d: %s", code, out)
		}
		var recs []struct {
			ID   string `json:"id"`
			Spec struct {
				Name     string `json:"name"`
				Kind     string `json:"kind"`
				CronExpr string `json:"cron_expr"`
				Model    string `json:"model"`
				Enabled  bool   `json:"enabled"`
				NextRun  string `json:"next_run"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(out, &recs); err != nil {
			return err
		}
		if len(recs) == 0 {
			fmt.Println("no scheduled tasks")
			return nil
		}
		for _, r := range recs {
			kind := r.Spec.Kind
			if kind == "" {
				kind = "chat"
			}
			on := "off"
			if r.Spec.Enabled {
				on = "on "
			}
			fmt.Printf("%-16s %s %-14s %-12s %-12s %s\n", r.ID, on, kind, r.Spec.CronExpr, r.Spec.Model, r.Spec.Name)
		}
		return nil
	case "add":
		fs := flag.NewFlagSet("scheduler add", flag.ExitOnError)
		name := fs.String("name", "", "task name (required)")
		cron := fs.String("cron", "", "cron expression, e.g. \"0 9 * * *\" (required)")
		kind := fs.String("kind", "chat", "chat | instance.start | instance.stop | backup")
		model := fs.String("model", "", "model id")
		prompt := fs.String("prompt", "", "prompt (chat kind)")
		instance := fs.String("instance", "", "instance id (instance.stop)")
		fs.Parse(reorderFlags(rest))
		body := map[string]any{
			"name": *name, "cron_expr": *cron, "kind": *kind,
			"model": *model, "prompt": *prompt, "instance": *instance,
			"enabled": true,
		}
		code, out, err := cc.Post("/api/v1/scheduler", body)
		if err != nil {
			return err
		}
		return printOut(code, out, 201)
	case "runs":
		fs := flag.NewFlagSet("scheduler runs", flag.ExitOnError)
		n := fs.Int("n", 20, "number of recent runs")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/scheduler/runs")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler runs returned %d: %s", code, out)
		}
		var runs []struct {
			Time       time.Time `json:"time"`
			Name       string    `json:"name"`
			Kind       string    `json:"kind"`
			Status     string    `json:"status"`
			DurationMs int64     `json:"durationMs"`
			Detail     string    `json:"detail"`
		}
		if err := json.Unmarshal(out, &runs); err != nil {
			return err
		}
		if len(runs) == 0 {
			fmt.Println("no scheduler runs yet (ring is in-memory, cleared on daemon restart)")
			return nil
		}
		if *n > 0 && *n < len(runs) {
			runs = runs[:*n]
		}
		fmt.Printf("%-19s %-20s %-14s %-6s %8s %s\n", "TIME", "TASK", "KIND", "STATUS", "MS", "DETAIL")
		for _, r := range runs {
			detail := r.Detail
			if len(detail) > 60 {
				detail = detail[:60] + "…"
			}
			fmt.Printf("%-19s %-20s %-14s %-6s %8d %s\n", r.Time.Local().Format("2006-01-02 15:04:05"), r.Name, r.Kind, r.Status, r.DurationMs, detail)
		}
		return nil
	case "run":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl scheduler run <id>")
		}
		code, out, err := cc.Post("/api/v1/scheduler/"+rest[0]+"/run", nil)
		if err != nil {
			return err
		}
		return printOut(code, out, 202)
	case "enable", "disable":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl scheduler %s <id>", sub)
		}
		code, out, err := cc.Post("/api/v1/scheduler/"+rest[0]+"/enabled", map[string]any{"enabled": sub == "enable"})
		if err != nil {
			return err
		}
		return printOut(code, out, 200)
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl scheduler delete <id>")
		}
		code, out, err := cc.Delete("/api/v1/scheduler/" + rest[0])
		if err != nil {
			return err
		}
		return printOut(code, out, 200)
	default:
		return fmt.Errorf("unknown scheduler subcommand %q", sub)
	}
}

// cmdRequests prints recent gateway requests (newest first).
func cmdRequests(args []string) error {
	fs := flag.NewFlagSet("requests", flag.ExitOnError)
	model := fs.String("model", "", "filter by model id")
	status := fs.String("status", "", "status class (4, 5) or exact code (404)")
	minMs := fs.Int("min-ms", 0, "minimum latency in ms")
	key := fs.String("key", "", "filter by API key prefix")
	instance := fs.String("instance", "", "filter by serving instance id")
	id := fs.String("id", "", "show the single request with this id")
	n := fs.Int("n", 20, "number of entries")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	if *id != "" {
		code, out, err := cc.Get("/api/v1/requests/" + url.PathEscape(*id))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("request %s: %s", *id, out)
		}
		fmt.Println(string(out))
		return nil
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(*n))
	if *model != "" {
		q.Set("model", *model)
	}
	if *status != "" {
		q.Set("status", *status)
	}
	if *minMs > 0 {
		q.Set("minMs", strconv.Itoa(*minMs))
	}
	if *key != "" {
		q.Set("key", *key)
	}
	if *instance != "" {
		q.Set("instance", *instance)
	}
	code, out, err := cc.Get("/api/v1/requests?" + q.Encode())
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("requests returned %d: %s", code, out)
	}
	var entries []struct {
		ID        string    `json:"id"`
		Time      time.Time `json:"time"`
		Model     string    `json:"model"`
		Instance  string    `json:"instance"`
		Key       string    `json:"key"`
		Status    int       `json:"status"`
		LatencyMs float64   `json:"latencyMs"`
		PromptTok int64     `json:"promptTok"`
		CompTok   int64     `json:"compTok"`
		Cost      float64   `json:"cost"`
		Error     string    `json:"error"`
		Stream    bool      `json:"stream"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("no requests yet")
		return nil
	}
	if *csvOut {
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "time", "model", "instance", "key", "status", "stream", "latency_ms", "prompt_tokens", "completion_tokens", "cost_usd", "error"})
		for _, e := range entries {
			cost := ""
			if e.Cost > 0 {
				cost = fmt.Sprintf("%.6f", e.Cost)
			}
			stream := "0"
			if e.Stream {
				stream = "1"
			}
			_ = w.Write([]string{e.ID, e.Time.UTC().Format(time.RFC3339), e.Model, e.Instance, e.Key,
				strconv.Itoa(e.Status), stream, strconv.Itoa(int(math.Round(e.LatencyMs))),
				strconv.FormatInt(e.PromptTok, 10), strconv.FormatInt(e.CompTok, 10), cost, e.Error})
		}
		w.Flush()
		return w.Error()
	}
	fmt.Printf("%-12s %-19s %-16s %-10s %-10s %5s %9s %7s %7s %-9s %-6s %s\n", "ID", "TIME", "MODEL", "INSTANCE", "KEY", "CODE", "MS", "IN", "OUT", "COST $", "STREAM", "ERROR")
	for _, e := range entries {
		stream := ""
		if e.Stream {
			stream = "stream"
		}
		cost := ""
		if e.Cost > 0 {
			cost = fmt.Sprintf("%.4f", e.Cost)
		}
		errTxt := e.Error
		if len(errTxt) > 48 {
			errTxt = errTxt[:48] + "…"
		}
		fmt.Printf("%-12s %-19s %-16s %-10s %-10s %5d %9.0f %7d %7d %-9s %-6s %s\n",
			e.ID, e.Time.Local().Format("2006-01-02 15:04:05"), e.Model, e.Instance, e.Key, e.Status, e.LatencyMs, e.PromptTok, e.CompTok, cost, stream, errTxt)
	}
	return nil
}

// cmdBackups lists, creates or deletes state snapshots in ~/.llmctl/backups.
func cmdBackups(args []string) error {
	sub := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		code, out, err := cc.Get("/api/v1/backups")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backups list returned %d: %s", code, out)
		}
		var bs []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Created string `json:"created"`
		}
		if err := json.Unmarshal(out, &bs); err != nil {
			return err
		}
		if len(bs) == 0 {
			fmt.Println("no snapshots (create one: llmctl backups create)")
			return nil
		}
		for _, b := range bs {
			fmt.Printf("%-34s %10d bytes  %s\n", b.Name, b.Size, b.Created)
		}
		return nil
	case "create":
		code, out, err := cc.Post("/api/v1/backups", nil)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 201 {
			return fmt.Errorf("backup create returned %d", code)
		}
		return nil
	case "restore":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backups restore <name>")
		}
		code, out, err := cc.Post("/api/v1/backups/"+url.PathEscape(rest[0])+"/restore", nil)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("restore returned %d", code)
		}
		fmt.Println("NOTE: restart the daemon (llmctl down && llmctl up) to load the restored state")
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backups delete <name>")
		}
		code, out, err := cc.Delete("/api/v1/backups/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("backup delete returned %d", code)
		}
		return nil
	default:
		return fmt.Errorf("unknown backups subcommand %q (list|create|restore|delete)", sub)
	}
}

// cmdMetrics prints the live gateway metrics snapshot: totals plus a
// per-model table (requests, errors, tokens, latency p50/p95, tok/s).
func cmdMetrics(args []string) error {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/metrics")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("metrics returned %d: %s", code, out)
	}
	var m struct {
		RequestsTotal int64 `json:"requests_total"`
		Tokens        struct {
			Prompt     int64 `json:"prompt"`
			Completion int64 `json:"completion"`
			Total      int64 `json:"total"`
		} `json:"tokens"`
		Latency map[string]float64 `json:"latency"`
		Status  map[string]int64   `json:"status_classes"`
		Models  map[string]struct {
			Requests int64   `json:"requests"`
			Errors   int64   `json:"errors"`
			Prompt   int64   `json:"prompt_tokens"`
			Comp     int64   `json:"completion_tokens"`
			P50      float64 `json:"latency_p50_ms"`
			P95      float64 `json:"latency_p95_ms"`
			Tps      float64 `json:"tokens_per_sec"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		return err
	}
	fmt.Printf("requests: %d   tokens: %d (in %d / out %d)\n", m.RequestsTotal, m.Tokens.Total, m.Tokens.Prompt, m.Tokens.Completion)
	classes := make([]string, 0, len(m.Status))
	for k, v := range m.Status {
		classes = append(classes, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(classes)
	fmt.Printf("status: %s\n", strings.Join(classes, " "))
	if len(m.Models) == 0 {
		fmt.Println("no model traffic yet")
		return nil
	}
	names := make([]string, 0, len(m.Models))
	for k := range m.Models {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Printf("\n%-16s %8s %6s %9s %9s %8s %8s %8s\n", "MODEL", "REQS", "ERRS", "IN", "OUT", "P50 MS", "P95 MS", "TOK/S")
	for _, n := range names {
		v := m.Models[n]
		fmt.Printf("%-16s %8d %6d %9d %9d %8.0f %8.0f %8.1f\n", n, v.Requests, v.Errors, v.Prompt, v.Comp, v.P50, v.P95, v.Tps)
	}
	return nil
}

// cmdUsage prints the daily per-model usage rollup (newest day first).
func cmdUsage(args []string) error {
	fs := flag.NewFlagSet("usage", flag.ExitOnError)
	n := fs.Int("n", 14, "number of days")
	key := fs.String("key", "", "show per-key daily usage for this key id instead of per-model")
	keysFlag := fs.Bool("keys", false, "show daily usage for all keys instead of per-model")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	if *key != "" {
		code, out, err := cc.Get("/api/v1/usage/keys?days=" + strconv.Itoa(*n) + "&key=" + url.QueryEscape(*key))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("usage returned %d: %s", code, out)
		}
		var kd map[string]map[string]struct {
			Requests int64   `json:"requests"`
			Errors   int64   `json:"errors"`
			Prompt   int64   `json:"prompt"`
			Comp     int64   `json:"comp"`
			Cost     float64 `json:"cost"`
		}
		if err := json.Unmarshal(out, &kd); err != nil {
			return err
		}
		if len(kd) == 0 {
			fmt.Printf("no usage recorded for key %s\n", *key)
			return nil
		}
		days := make([]string, 0, len(kd))
		for d := range kd {
			days = append(days, d)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(days)))
		for _, day := range days {
			for id, u := range kd[day] {
				fmt.Printf("%s  key %s  %6d reqs %4d err %10d in %10d out", day, id, u.Requests, u.Errors, u.Prompt, u.Comp)
				if u.Cost > 0 {
					fmt.Printf("  $%.4f", u.Cost)
				}
				fmt.Println()
			}
		}
		return nil
	}
	if *keysFlag {
		code, out, err := cc.Get("/api/v1/usage/keys?days=" + strconv.Itoa(*n))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("usage returned %d: %s", code, out)
		}
		var kd map[string]map[string]struct {
			Requests int64   `json:"requests"`
			Errors   int64   `json:"errors"`
			Prompt   int64   `json:"prompt"`
			Comp     int64   `json:"comp"`
			Cost     float64 `json:"cost"`
		}
		if err := json.Unmarshal(out, &kd); err != nil {
			return err
		}
		if len(kd) == 0 {
			fmt.Println("no per-key usage recorded yet")
			return nil
		}
		names := map[string]string{} // key id -> name for readable output
		if code, kout, err := cc.Get("/api/v1/keys"); err == nil && code == 200 {
			var kl []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if json.Unmarshal(kout, &kl) == nil {
				for _, k := range kl {
					names[k.ID] = k.Name
				}
			}
		}
		days := make([]string, 0, len(kd))
		for d := range kd {
			days = append(days, d)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(days)))
		if *csvOut {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"date", "key_id", "key_name", "requests", "errors", "prompt_tokens", "completion_tokens", "cost_usd"})
			for _, day := range days {
				ids := make([]string, 0, len(kd[day]))
				for id := range kd[day] {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					u := kd[day][id]
					_ = w.Write([]string{day, id, names[id], strconv.FormatInt(u.Requests, 10), strconv.FormatInt(u.Errors, 10),
						strconv.FormatInt(u.Prompt, 10), strconv.FormatInt(u.Comp, 10), strconv.FormatFloat(u.Cost, 'f', 6, 64)})
				}
			}
			w.Flush()
			return w.Error()
		}
		for _, day := range days {
			ids := make([]string, 0, len(kd[day]))
			for id := range kd[day] {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				u := kd[day][id]
				name := names[id]
				if name == "" {
					name = "(revoked)"
				}
				fmt.Printf("%s  %-14s %-16s %6d reqs %4d err %10d in %10d out", day, id[:min(12, len(id))], name, u.Requests, u.Errors, u.Prompt, u.Comp)
				if u.Cost > 0 {
					fmt.Printf("  $%.4f", u.Cost)
				}
				fmt.Println()
			}
		}
		return nil
	}
	code, out, err := cc.Get("/api/v1/usage/daily?days=" + strconv.Itoa(*n))
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("usage returned %d: %s", code, out)
	}
	var daily map[string]map[string]struct {
		Requests int64   `json:"requests"`
		Errors   int64   `json:"errors"`
		Prompt   int64   `json:"prompt"`
		Comp     int64   `json:"comp"`
		Cost     float64 `json:"cost"`
	}
	if err := json.Unmarshal(out, &daily); err != nil {
		return err
	}
	if len(daily) == 0 {
		fmt.Println("no usage recorded yet")
		return nil
	}
	days := make([]string, 0, len(daily))
	for d := range daily {
		days = append(days, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	if *csvOut {
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"date", "model", "requests", "errors", "prompt_tokens", "completion_tokens", "cost_usd"})
		for _, day := range days {
			models := daily[day]
			names := make([]string, 0, len(models))
			for m := range models {
				names = append(names, m)
			}
			sort.Strings(names)
			for _, m := range names {
				u := models[m]
				_ = w.Write([]string{day, m, strconv.FormatInt(u.Requests, 10), strconv.FormatInt(u.Errors, 10),
					strconv.FormatInt(u.Prompt, 10), strconv.FormatInt(u.Comp, 10), strconv.FormatFloat(u.Cost, 'f', 6, 64)})
			}
		}
		w.Flush()
		return w.Error()
	}
	for _, day := range days {
		fmt.Printf("%s\n", day)
		models := daily[day]
		names := make([]string, 0, len(models))
		for m := range models {
			names = append(names, m)
		}
		sort.Strings(names)
		for _, m := range names {
			u := models[m]
			name := m
			if name == "" {
				name = "(unknown)"
			}
			fmt.Printf("  %-20s %6d reqs %4d err %10d in %10d out", name, u.Requests, u.Errors, u.Prompt, u.Comp)
			if u.Cost > 0 {
				fmt.Printf("  $%.4f", u.Cost)
			}
			fmt.Println()
		}
	}
	return nil
}

// cmdEvents prints the recent daemon events feed (newest first).
func cmdEvents(args []string) error {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	n := fs.Int("n", 20, "number of events")
	kind := fs.String("kind", "", "only this event kind, e.g. instance.crashed")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	u := "/api/v1/events?limit=" + strconv.Itoa(*n)
	if *kind != "" {
		u += "&kind=" + url.QueryEscape(*kind)
	}
	code, out, err := cc.Get(u)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("events returned %d: %s", code, out)
	}
	var evs []struct {
		Time     time.Time `json:"time"`
		Kind     string    `json:"kind"`
		Instance string    `json:"instance"`
		Model    string    `json:"model"`
	}
	if err := json.Unmarshal(out, &evs); err != nil {
		return err
	}
	if len(evs) == 0 {
		fmt.Println("no events since daemon start")
		return nil
	}
	fmt.Printf("%-19s %-26s %-16s %s\n", "TIME", "EVENT", "INSTANCE", "MODEL")
	for _, e := range evs {
		fmt.Printf("%-19s %-26s %-16s %s\n",
			e.Time.Local().Format("2006-01-02 15:04:05"), e.Kind, e.Instance, e.Model)
	}
	return nil
}

func cmdKey(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl key <generate|show|list|revoke|prune|reset-usage>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "generate":
		fs := flag.NewFlagSet("key generate", flag.ExitOnError)
		quota := fs.Int64("quota", 0, "token quota (0 = unlimited)")
		maxReq := fs.Int64("maxreq", 0, "total request cap (0 = unlimited)")
		models := fs.String("models", "", "comma-separated allowed model ids (empty = all)")
		expires := fs.Int("expires", 0, "expire after N days (0 = never)")
		budget := fs.Float64("budget", 0, "daily USD budget (0 = unlimited)")
		rps := fs.Float64("rps", 0, "per-key rate limit, req/s (0 = gateway default)")
		fs.Parse(reorderFlags(rest))
		name := ""
		if fs.NArg() > 0 {
			name = fs.Arg(0)
		}
		body := map[string]any{"name": name}
		if *quota > 0 {
			body["tokenQuota"] = *quota
		}
		if *maxReq > 0 {
			body["maxRequests"] = *maxReq
		}
		if *models != "" {
			list := []string{}
			for _, m := range strings.Split(*models, ",") {
				if m = strings.TrimSpace(m); m != "" {
					list = append(list, m)
				}
			}
			body["allowedModels"] = list
		}
		if *expires > 0 {
			body["expiresInDays"] = *expires
		}
		if *budget > 0 {
			body["dailyCostBudgetUSD"] = *budget
		}
		if *rps > 0 {
			body["rateRPS"] = *rps
		}
		code, out, err := cc.Post("/api/v1/keys", body)
		if err != nil {
			return err
		}
		var r map[string]any
		json.Unmarshal(out, &r)
		if code != 201 {
			return fmt.Errorf("create returned %d: %s", code, string(out))
		}
		fmt.Println("key:", r["key"])
		fmt.Println("name:", r["name"])
		fmt.Println("NOTE: shown only once — store it now.")
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key show <id>")
		}
		code, out, err := cc.Get("/api/v1/keys/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key show returned %d: %s", code, out)
		}
		var kv struct {
			ID                 string     `json:"id"`
			Name               string     `json:"name"`
			Prefix             string     `json:"prefix"`
			CreatedAt          time.Time  `json:"createdAt"`
			TokenQuota         int64      `json:"tokenQuota"`
			UsedTok            int64      `json:"usedTokens"`
			UsedCost           float64    `json:"usedCost"`
			UsedReqs           int64      `json:"usedRequests"`
			MaxRequests        int64      `json:"maxRequests"`
			AllowedModels      []string   `json:"allowedModels"`
			ExpiresAt          *time.Time `json:"expiresAt"`
			LastUsed           *time.Time `json:"lastUsed"`
			DailyCostBudgetUSD float64    `json:"dailyCostBudgetUSD"`
			TodayCost          float64    `json:"todayCost"`
		}
		if err := json.Unmarshal(out, &kv); err != nil {
			return err
		}
		fmt.Printf("id:       %s\nname:     %s\nprefix:   %s…\ncreated:  %s\n", kv.ID, kv.Name, kv.Prefix, kv.CreatedAt.Local().Format("2006-01-02 15:04"))
		fmt.Printf("usage:    %d tok · %d reqs · $%.4f\n", kv.UsedTok, kv.UsedReqs, kv.UsedCost)
		if kv.TokenQuota > 0 || kv.MaxRequests > 0 {
			fmt.Printf("quota:    ")
			if kv.TokenQuota > 0 {
				fmt.Printf("%d/%d tok ", kv.UsedTok, kv.TokenQuota)
			}
			if kv.MaxRequests > 0 {
				fmt.Printf("%d/%d reqs", kv.UsedReqs, kv.MaxRequests)
			}
			fmt.Println()
		}
		if len(kv.AllowedModels) > 0 {
			fmt.Printf("models:   %s\n", strings.Join(kv.AllowedModels, ", "))
		}
		if kv.ExpiresAt != nil {
			fmt.Printf("expires:  %s\n", kv.ExpiresAt.Local().Format("2006-01-02 15:04"))
		}
		if kv.LastUsed != nil {
			fmt.Printf("last used:%s\n", kv.LastUsed.Local().Format("2006-01-02 15:04"))
		}
		fmt.Printf("today:    $%.4f", kv.TodayCost)
		if kv.DailyCostBudgetUSD > 0 {
			fmt.Printf(" / $%.2f budget", kv.DailyCostBudgetUSD)
		}
		fmt.Println()
		return nil
	case "list":
		_, out, err := cc.Get("/api/v1/keys")
		if err != nil {
			return err
		}
		var list []struct {
			ID                 string     `json:"id"`
			Name               string     `json:"name"`
			Prefix             string     `json:"prefix"`
			TokenQuota         int64      `json:"tokenQuota"`
			UsedTok            int64      `json:"usedTokens"`
			UsedCost           float64    `json:"usedCost"`
			MaxRequests        int64      `json:"maxRequests"`
			UsedReqs           int64      `json:"usedRequests"`
			ExpiresAt          *time.Time `json:"expiresAt"`
			LastUsed           *time.Time `json:"lastUsed"`
			DailyCostBudgetUSD float64    `json:"dailyCostBudgetUSD"`
			TodayCost          float64    `json:"todayCost"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no keys")
			return nil
		}
		fmt.Printf("%-17s %-14s %-10s %10s %9s %8s %-12s %-12s %s\n", "ID", "NAME", "PREFIX", "TOKENS", "COST $", "REQS", "EXPIRES", "LAST USED", "QUOTA")
		for _, k := range list {
			quota := ""
			if k.TokenQuota > 0 {
				quota = fmt.Sprintf("tok %d/%d", k.UsedTok, k.TokenQuota)
			}
			if k.MaxRequests > 0 {
				if quota != "" {
					quota += ", "
				}
				quota += fmt.Sprintf("req %d/%d", k.UsedReqs, k.MaxRequests)
			}
			if k.DailyCostBudgetUSD > 0 {
				if quota != "" {
					quota += ", "
				}
				quota += fmt.Sprintf("$%.4f/%.2f/day", k.TodayCost, k.DailyCostBudgetUSD)
			}
			expires := "-"
			if k.ExpiresAt != nil {
				if time.Now().After(*k.ExpiresAt) {
					expires = "expired"
				} else {
					expires = k.ExpiresAt.Local().Format("2006-01-02")
				}
			}
			last := "-"
			if k.LastUsed != nil {
				last = k.LastUsed.Local().Format("01-02 15:04")
			}
			fmt.Printf("%-17s %-14s %-10s %10d %9.4f %8d %-12s %-12s %s\n",
				k.ID, k.Name, k.Prefix+"…", k.UsedTok, k.UsedCost, k.UsedReqs, expires, last, quota)
		}
		return nil
	case "revoke":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key revoke <id>")
		}
		code, out, err := cc.Delete("/api/v1/keys/" + rest[0])
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("revoke returned %d", code)
		}
		return nil
	case "prune":
		code, out, err := cc.Post("/api/v1/keys/prune", nil)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prune returned %d: %s", code, out)
		}
		var r struct {
			Removed []string `json:"removed"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			return err
		}
		fmt.Printf("removed %d expired key(s)\n", len(r.Removed))
		for _, id := range r.Removed {
			fmt.Println(" ", id)
		}
		return nil
	case "reset-usage":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key reset-usage <id>")
		}
		code, out, err := cc.Post("/api/v1/keys/"+rest[0]+"/reset-usage", map[string]any{})
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("reset-usage returned %d", code)
		}
		return nil
	default:
		return fmt.Errorf("unknown key subcommand: %s", sub)
	}
}

// stringList is a repeatable --var k=v flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
func (s *stringList) Map() map[string]string {
	m := map[string]string{}
	for _, kv := range *s {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func urlEscape(s string) string {
	repl := strings.NewReplacer("%", "%25", " ", "+", "&", "%26", "=", "%3D")
	return repl.Replace(s)
}

func cmdAutoStart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl autostart <list|add|remove>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		_, out, err := cc.Get("/api/v1/autostart")
		if err != nil {
			return err
		}
		var list []map[string]any
		json.Unmarshal(out, &list)
		if len(list) == 0 {
			fmt.Println("no auto-start entries")
			return nil
		}
		for _, s := range list {
			fmt.Printf("%-24s model=%-24s backend=%-12s port=%v\n", s["name"], s["modelId"], s["backendId"], s["port"])
		}
		return nil
	case "add":
		fs := flag.NewFlagSet("autostart add", flag.ExitOnError)
		backend := fs.String("backend", "", "backend id")
		instance := fs.String("instance", "", "instance name")
		port := fs.Int("port", 0, "fixed port (0 = auto)")
		var vars stringList
		fs.Var(&vars, "var", "parameter k=v (repeatable)")
		fs.Parse(reorderFlags(rest))
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl autostart add <model> [--backend id] [--instance name] [--port N] [--var k=v ...]")
		}
		body := map[string]any{
			"modelId":   fs.Arg(0),
			"backendId": *backend,
			"name":      *instance,
			"port":      *port,
			"vars":      vars.Map(),
		}
		code, out, err := cc.Post("/api/v1/autostart", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("add failed: %s", string(out))
		}
		fmt.Printf("auto-start enabled for %q (relaunches when the daemon starts)\n", fs.Arg(0))
		return nil
	case "remove":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl autostart remove <model>")
		}
		code, out, err := cc.Delete("/api/v1/autostart/" + urlEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("remove failed: %s", string(out))
		}
		fmt.Printf("auto-start removed for %q\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown autostart subcommand: %s", sub)
	}
}

func cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	modelFlag := fs.String("model", "", "model id to chat with")
	keyFlag := fs.String("key", "", "API key (if auth is enabled)")
	fs.Parse(reorderFlags(args))

	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}

	// List running instances to pick a model.
	_, out, err := cc.Get("/api/v1/instances")
	if err != nil {
		return err
	}
	var insts []map[string]any
	if err := json.Unmarshal(out, &insts); err != nil {
		return err
	}
	running := []map[string]any{}
	for _, i := range insts {
		if fmt.Sprint(i["status"]) == "running" {
			running = append(running, i)
		}
	}
	if len(running) == 0 {
		return fmt.Errorf("no running instances — start one with: llmctl start <model-id>")
	}

	model := *modelFlag
	if model == "" {
		if len(running) == 1 {
			model = fmt.Sprint(running[0]["modelId"])
		} else {
			fmt.Println("Running instances:")
			for i, inst := range running {
				fmt.Printf("  %d) %s (%s)\n", i+1, inst["modelId"], inst["backendId"])
			}
			fi, _ := os.Stdin.Stat()
			if fi.Mode()&os.ModeCharDevice == 0 {
				return fmt.Errorf("multiple instances running — specify --model ID")
			}
			fmt.Print("Pick instance number: ")
			sc := bufio.NewScanner(os.Stdin)
			sc.Scan()
			sel, serr := strconv.Atoi(strings.TrimSpace(sc.Text()))
			if serr != nil || sel < 1 || sel > len(running) {
				return fmt.Errorf("invalid selection")
			}
			model = fmt.Sprint(running[sel-1]["modelId"])
		}
	}

	// Get API key.
	key := *keyFlag
	if key == "" {
		fi, _ := os.Stdin.Stat()
		if fi.Mode()&os.ModeCharDevice != 0 {
			// stdin is a terminal: prompt interactively
			fmt.Print("API key (or leave blank if auth is off): ")
			sc := bufio.NewScanner(os.Stdin)
			sc.Scan()
			key = strings.TrimSpace(sc.Text())
		}
	}

	fmt.Printf("\nChatting with model %q. Type your message (Ctrl-D or /quit to exit).\n\n", model)

	reader := bufio.NewReader(os.Stdin)
	history := []map[string]any{}
	outW := bufio.NewWriter(os.Stdout)

	for {
		outW.WriteString("> ")
		outW.Flush()
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println()
				break
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "/quit" || line == "/exit" {
			break
		}

		// Append user message.
		history = append(history, map[string]any{"role": "user", "content": line})

		body := map[string]any{
			"model":    model,
			"messages": history,
			"stream":   true,
		}
		b, _ := json.Marshal(body)

		req, err := http.NewRequest(http.MethodPost, cc.Base+"/v1/chat/completions", strings.NewReader(string(b)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}

		hc := &http.Client{Timeout: 10 * time.Minute}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("chat returned %d: %s", resp.StatusCode, string(bodyBytes))
		}
		defer resp.Body.Close()

		outW.WriteString("< ")
		outW.Flush()
		var acc strings.Builder
		rd := bufio.NewReader(resp.Body)
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			l = strings.TrimSpace(l)
			if !strings.HasPrefix(l, "data:") {
				continue
			}
			data := strings.TrimSpace(l[len("data:"):])
			if data == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				outW.WriteString(chunk.Choices[0].Delta.Content)
				outW.Flush()
				acc.WriteString(chunk.Choices[0].Delta.Content)
			}
		}
		outW.WriteString("\n")
		outW.Flush()

		// Append assistant message to history.
		if acc.Len() > 0 {
			history = append(history, map[string]any{"role": "assistant", "content": acc.String()})
		}
	}
	outW.Flush()
	return nil
}

// cmdBackup exports the llmctl state directory (config, chats, knowledge
// bases, memory, skills, tools, scheduler, plugins) to a tar.gz file. Works
// with the daemon stopped; heavy regenerable data (binaries, models, logs) is
// excluded.
func cmdBackup(args []string) error {
	out := ""
	if len(args) > 0 {
		out = args[0]
	}
	if out == "" {
		out = fmt.Sprintf("llmctl-backup-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := backup.Create(f); err != nil {
		os.Remove(out)
		return err
	}
	st, _ := f.Stat()
	fmt.Printf("backup written to %s (%s)\n", out, fmtSize(st.Size()))
	return nil
}

// cmdRestore imports a tar.gz backup into the llmctl state directory.
func cmdRestore(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl restore <file.tar.gz>")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := backup.Restore(f)
	if err != nil {
		return err
	}
	fmt.Printf("restored %d file(s) into %s\n", n, paths.Home())
	return nil
}

func fmtSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
