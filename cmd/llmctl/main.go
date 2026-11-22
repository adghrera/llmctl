// Command llmctl is the CLI for the llmctl daemon. It is a thin client over
// the management API: `llmctl up` runs the daemon (gateway + API + UI on one
// port); every other command talks to the running daemon.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	case "webhooks":
		err = cmdWebhooks(args)
	case "prompts":
		err = cmdPrompts(args)
	case "chats":
		err = cmdChats(args)
	case "skills":
		err = cmdSkills(args)
	case "memory":
		err = cmdMemory(args)
	case "kb":
		err = cmdKb(args)
	case "tools":
		err = cmdTools(args)
	case "mcp":
		err = cmdMcp(args)
	case "cost":
		err = cmdCost(args)
	case "files":
		err = cmdFiles(args)
	case "settings":
		err = cmdSettings(args)
	case "batch":
		err = cmdBatch(args)
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
  llmctl status [-json] [-watch N]
  llmctl doctor [-json] [-quiet] [-check NAME]...
  llmctl ps [-watch N] [-json]
  llmctl backends list [-json]
  llmctl backends install <id> [--asset <hint>]
  llmctl backends uninstall <id>
  llmctl backends add <plugin.json>
  llmctl backends remove <id>
  llmctl models list
  llmctl models show <id>
  llmctl models probe
  llmctl models ctx <id> <tokens> | price <id> <in/M> <out/M> | idle <id> <min|never>
  llmctl models aliases
  llmctl models alias <name> <model-id>
  llmctl models unalias <name>
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
  llmctl key generate [name] [-json]
  llmctl key show <id> [-json]
  llmctl key note <id> <text...>
  llmctl key update <id> [-name X] [-quota N] [-models a,b] …
  llmctl key disable <id> / llmctl key enable <id>
  llmctl key rotate <id> [-json]
  llmctl key list
  llmctl key revoke <id>
  llmctl key reset-usage <id>
  llmctl autostart list
  llmctl autostart add <model> [--backend ID] [--instance NAME] [--port N] [--var k=v ...]
  llmctl autostart remove <model>
  llmctl scheduler list
  llmctl scheduler show <id> [-json]
  llmctl scheduler add --name N --cron "0 9 * * *" [--kind chat|instance.start|instance.stop|backup] [--model ID] [--prompt TEXT] [--instance ID]
  llmctl scheduler run|enable|disable|delete <id>
  llmctl scheduler runs [-n 20]
  llmctl requests [--model ID] [--status 4|404] [--min-ms N] [--key PREFIX] [--instance ID] [--id ID] [-n 20] [-json] [-watch N] [-stats [-by model|key|instance]]
  llmctl backups [list|create|restore <name>|verify <name>|delete <name>]
  llmctl usage [-n 14] [-key ID] [-keys] [-model ID] [--csv] [--json]
  llmctl metrics [-watch N] [-json]
  llmctl events [-n 20] [-csv] [-json]
  llmctl webhooks [-n 20] [-json]   recent webhook delivery attempts
  llmctl prompts [filter] [-category X] [-json] | llmctl prompts show <id>
  llmctl chats [filter] [-n 20] [-json] | llmctl chats show <id> [-json]
  llmctl skills [filter] [-json] | skills show|enable|disable|delete <id>
  llmctl memory [filter] [-n 30] [-json] | memory show|add|delete …
  llmctl kb [filter] [-json] | kb show|create|docs|add-doc|delete …
  llmctl tools [-json] | tools show|install|enable|disable|run|delete <id>
  llmctl mcp [-json] | mcp add|remove|enable|disable|test <id …>
  llmctl cost [-keys] [-json]
  llmctl files [list|upload <path>|cat <id>|delete <id>] [-json]
  llmctl settings [-json] | llmctl settings set <flags> | settings test-webhook
  llmctl batch -model ID [-system S] [-key K] [-concurrency N] [-file F|piped stdin|prompts…] [-json]
  llmctl chat [--model ID] [--key KEY]
  llmctl backup [file.tar.gz]     export state (config/chats/KBs/memory/…)
  llmctl backup verify <file>     check any local archive offline
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
	watch := fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	printOnce := func() error {
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
	if *watch <= 0 {
		return printOnce()
	}
	// Live view: clear + reprint every interval until interrupted (F216).
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live status — refreshing every %ds (Ctrl-C to stop)\n\n", *watch)
		if err := printOnce(); err != nil {
			return err
		}
		time.Sleep(time.Duration(*watch) * time.Second)
	}
}

// cmdDoctor runs the daemon-side health sweep and prints one line per check
// (F146). Exit code is non-zero when any check errored.
func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw doctor JSON (for scripts)")
	quiet := fs.Bool("quiet", false, "print only warn/error checks (for cron/monitoring)")
	var only stringList
	fs.Var(&only, "check", "only checks whose name contains this (repeatable, F259)")
	fs.Parse(reorderFlags(args))
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
	// -check filter (F259): keep only checks whose name contains one of the
	// given substrings; the exit status then reflects just those.
	if len(only) > 0 {
		var keep []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		}
		for _, c := range res.Checks {
			for _, o := range only {
				if strings.Contains(c.Name, o) {
					keep = append(keep, c)
					break
				}
			}
		}
		if len(keep) == 0 {
			return fmt.Errorf("no checks match: %s", strings.Join(only, ", "))
		}
		res.Checks = keep
		res.OK = true
		for _, c := range keep {
			if c.Status == "error" {
				res.OK = false
			}
		}
	}
	if *jsonOut {
		if len(only) > 0 {
			b, err := json.Marshal(res)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
		} else {
			fmt.Print(string(out))
		}
		if !res.OK {
			return fmt.Errorf("doctor: some checks failed")
		}
		return nil
	}
	sym := map[string]string{"ok": "[ok]  ", "warn": "[warn]", "error": "[fail]"}
	warns := 0
	for _, c := range res.Checks {
		if c.Status == "warn" {
			warns++
		}
		if *quiet && c.Status == "ok" {
			continue
		}
		s, ok := sym[c.Status]
		if !ok {
			s = c.Status
		}
		fmt.Printf("%s %-9s %s\n", s, c.Name, c.Detail)
	}
	if !res.OK {
		return fmt.Errorf("doctor: some checks failed")
	}
	if *quiet {
		if warns > 0 {
			fmt.Printf("doctor: %d warning(s), no errors\n", warns)
		} else {
			fmt.Println("doctor: all checks passed")
		}
	}
	return nil
}

func cmdPS(args []string) error {
	fs := flag.NewFlagSet("ps", flag.ExitOnError)
	watch := fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once)")
	jsonOut := fs.Bool("json", false, "print the raw instances JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	printOnce := func() error {
		_, out, err := cc.Get("/api/v1/instances")
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		return printPS(out)
	}
	if *watch <= 0 {
		return printOnce()
	}
	// Live view: clear + reprint every interval until interrupted (F194).
	for {
		_, out, err := cc.Get("/api/v1/instances")
		if err != nil {
			return err
		}
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live instances — refreshing every %ds (Ctrl-C to stop)\n\n", *watch)
		if err := printPS(out); err != nil {
			return err
		}
		time.Sleep(time.Duration(*watch) * time.Second)
	}
}

func printPS(out []byte) error {
	var list []struct {
		ID           string    `json:"id"`
		ModelID      string    `json:"modelId"`
		BackendID    string    `json:"backendId"`
		Transport    string    `json:"transport"`
		Port         int       `json:"port"`
		Status       string    `json:"status"`
		StartedAt    time.Time `json:"startedAt"`
		Served       int64     `json:"served"`
		RestartCount int       `json:"restartCount"`
		Note         string    `json:"note"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no instances (start one: llmctl start <model-id>)")
		return nil
	}
	fmt.Printf("%-28s %-24s %-12s %-8s %-7s %-9s %-8s %6s %8s %s\n", "INSTANCE", "MODEL", "BACKEND", "TRANS", "PORT", "STATUS", "UPTIME", "SERVED", "RESTARTS", "NOTE")
	for _, i := range list {
		up := ""
		if i.Status == "running" && !i.StartedAt.IsZero() {
			up = uptimeShort(time.Since(i.StartedAt))
		}
		note := i.Note
		if len(note) > 40 {
			note = note[:37] + "..."
		}
		fmt.Printf("%-28s %-24s %-12s %-8s %-7v %-9s %-8s %6d %8d %s\n",
			i.ID, i.ModelID, i.BackendID, i.Transport, i.Port, i.Status, up, i.Served, i.RestartCount, note)
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
		fs := flag.NewFlagSet("backends list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw backends JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/backends")
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
		return fmt.Errorf("usage: llmctl models <list|show|probe|price|ctx|idle|install|remove|hf-search|hf-files>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("models list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw models JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/models")
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
		var total int64
		for _, m := range list {
			total += m.SizeBytes
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
			fmt.Printf("%-40s %-12s %-10s %-9s %-14s %-7s %v\n", m.ID, m.BackendID, fmtSize(m.SizeBytes), ctxv, price, idle, m.Running)
		}
		fmt.Printf("%-40s %-12s %-10s\n", "total: "+strconv.Itoa(len(list))+" model(s)", "", fmtSize(total))
		return nil
	case "probe":
		pfs := flag.NewFlagSet("models probe", flag.ExitOnError)
		pjson := pfs.Bool("json", false, "print the probe JSON (for scripts)")
		pfs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/models/probe")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("probe returned %d: %s", code, out)
		}
		if *pjson {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"ownedBy"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no models served (start an instance: llmctl start <model>)")
			return nil
		}
		fmt.Printf("%-20s %s\n", "ID", "SOURCE")
		for _, m := range list {
			fmt.Printf("%-20s %s\n", m.ID, m.OwnedBy)
		}
		return nil
	case "idle":
		ifs := flag.NewFlagSet("models idle", flag.ExitOnError)
		ifs.Parse(reorderFlags(rest))
		if ifs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl models idle <id> <minutes|never>  (never = -1, 0 = global default)")
		}
		n, err := strconv.Atoi(ifs.Arg(1))
		if ifs.Arg(1) == "never" {
			n, err = -1, nil
		}
		if err != nil {
			return fmt.Errorf("invalid minutes: %s", ifs.Arg(1))
		}
		code, outb, err := cc.Do("PATCH", "/api/v1/models/"+url.PathEscape(ifs.Arg(0))+"/idle", map[string]any{"idleUnloadMin": n})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("idle returned %d: %s", code, outb)
		}
		fmt.Printf("idleUnloadMin %d set for %s\n", n, ifs.Arg(0))
		return nil
	case "aliases":
		fs := flag.NewFlagSet("aliases", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw aliases JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/aliases")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("aliases returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var als []struct {
			Alias string `json:"alias"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(out, &als); err != nil {
			return err
		}
		if len(als) == 0 {
			fmt.Println("no aliases (add one: llmctl models alias <name> <model-id>)")
			return nil
		}
		for _, a := range als {
			fmt.Printf("%-16s -> %s\n", a.Alias, a.Model)
		}
		return nil
	case "alias":
		if len(rest) < 2 {
			return fmt.Errorf("usage: llmctl models alias <name> <model-id>")
		}
		code, out, err := cc.Post("/api/v1/aliases", map[string]string{"alias": rest[0], "model": rest[1]})
		if err != nil {
			return err
		}
		if code != 200 && code != 201 {
			return fmt.Errorf("alias returned %d: %s", code, out)
		}
		fmt.Printf("alias %s -> %s\n", rest[0], rest[1])
		return nil
	case "unalias":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models unalias <name>")
		}
		code, out, err := cc.Delete("/api/v1/aliases/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("unalias returned %d: %s", code, out)
		}
		fmt.Printf("removed alias %s\n", rest[0])
		return nil
	case "show":
		sfs := flag.NewFlagSet("models show", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw model JSON (for scripts)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models show <id> [-json]")
		}
		code, out, err := cc.Get("/api/v1/models/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models show returned %d: %s", code, out)
		}
		if *sjson {
			fmt.Print(string(out))
			return nil
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
		fmt.Printf("id:       %s\nbackend:  %s\npath:     %s\nsize:     %s\nctx:      %s\nprice:    %s\nidle:     %s\nstatus:   %s\n", mv.ID, mv.BackendID, mv.Path, fmtSize(mv.SizeBytes), ctxv, price, idle, status)
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
		hfs := flag.NewFlagSet("models hf-search", flag.ExitOnError)
		hjson := hfs.Bool("json", false, "print the raw search JSON (for scripts)")
		hfs.Parse(reorderFlags(rest))
		if hfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models hf-search <query> [-json]")
		}
		q := strings.Join(hfs.Args(), " ")
		_, out, err := cc.Get("/api/v1/hf/search?q=" + urlEscape(q))
		if err != nil {
			return err
		}
		if *hjson {
			fmt.Print(string(out))
			return nil
		}
		var res []map[string]any
		json.Unmarshal(out, &res)
		for _, m := range res {
			fmt.Println(m["modelId"])
		}
		return nil
	case "hf-files":
		ffs := flag.NewFlagSet("models hf-files", flag.ExitOnError)
		fjson := ffs.Bool("json", false, "print the raw files JSON (for scripts)")
		ffs.Parse(reorderFlags(rest))
		if ffs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models hf-files <repo> [-json]")
		}
		_, out, err := cc.Get("/api/v1/hf/files?repo=" + urlEscape(ffs.Arg(0)))
		if err != nil {
			return err
		}
		if *fjson {
			fmt.Print(string(out))
			return nil
		}
		var files []map[string]any
		json.Unmarshal(out, &files)
		for _, f := range files {
			// The HF proxy returns Go field names (Path/SizeBytes); accept
			// lowercase too (F257).
			path := fmt.Sprint(f["Path"])
			if path == "<nil>" {
				path = fmt.Sprint(f["path"])
			}
			size, _ := f["SizeBytes"].(float64)
			if size == 0 {
				size, _ = f["sizeBytes"].(float64)
			}
			fmt.Printf("%s  (%s)\n", path, fmtSize(int64(size)))
		}
		return nil
	default:
		return fmt.Errorf("unknown models subcommand: %s (list|show|probe|ctx|price|idle|aliases|alias|unalias|install|remove)", sub)
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
// untilStr renders a future time as "in 5m"-style relative text (F238);
// past or zero times render as "now"/"due".
func untilStr(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	s := int64(time.Until(t).Seconds())
	switch {
	case s <= 0:
		return "due"
	case s < 60:
		return fmt.Sprintf("in %ds", s)
	case s < 3600:
		return fmt.Sprintf("in %dm", s/60)
	case s < 86400:
		return fmt.Sprintf("in %dh", s/3600)
	default:
		return fmt.Sprintf("in %dd", s/86400)
	}
}

func cmdScheduler(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl scheduler <list|show|add|run|enable|disable|delete> [id] [flags]")
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
		fs := flag.NewFlagSet("scheduler list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw scheduler JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/scheduler")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler list returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
			LastRun *struct {
				Time   string `json:"time"`
				Status string `json:"status"`
			} `json:"lastRun"`
		}
		if err := json.Unmarshal(out, &recs); err != nil {
			return err
		}
		if len(recs) == 0 {
			fmt.Println("no scheduled tasks")
			return nil
		}
		fmt.Printf("%-16s %-4s %-12s %-12s %-12s %-10s %-8s %s\n", "ID", "ON", "KIND", "CRON", "MODEL", "LAST", "NEXT", "NAME")
		for _, r := range recs {
			kind := r.Spec.Kind
			if kind == "" {
				kind = "chat"
			}
			on := "off"
			if r.Spec.Enabled {
				on = "on "
			}
			last := "—"
			if r.LastRun != nil {
				last = r.LastRun.Status
				if t, err := time.Parse(time.RFC3339, r.LastRun.Time); err == nil {
					last += " " + t.Local().Format("01-02 15:04")
				}
			}
			next := "—"
			if t, err := time.Parse(time.RFC3339, r.Spec.NextRun); err == nil {
				next = untilStr(t)
			}
			fmt.Printf("%-16s %-4s %-12s %-12s %-12s %-10s %-8s %s\n", r.ID, on, kind, r.Spec.CronExpr, r.Spec.Model, last, next, r.Spec.Name)
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
		webhook := fs.String("webhook", "", "URL POSTed a JSON result on each run")
		keep := fs.Int("keep", 0, "snapshots to retain (backup kind; 0 = default)")
		fs.Parse(reorderFlags(rest))
		body := map[string]any{
			"name": *name, "cron_expr": *cron, "kind": *kind,
			"model": *model, "prompt": *prompt, "instance": *instance,
			"enabled": true,
		}
		if *webhook != "" {
			body["webhook_url"] = *webhook
		}
		if *keep > 0 {
			body["keep"] = *keep
		}
		code, out, err := cc.Post("/api/v1/scheduler", body)
		if err != nil {
			return err
		}
		return printOut(code, out, 201)
	case "show":
		sfs := flag.NewFlagSet("scheduler show", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw task JSON (for scripts)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl scheduler show <id> [-json]")
		}
		code, out, err := cc.Get("/api/v1/scheduler/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler %s: %s", sfs.Arg(0), out)
		}
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		var rec struct {
			ID   string `json:"id"`
			Spec struct {
				ID         string    `json:"id"`
				Name       string    `json:"name"`
				CronExpr   string    `json:"cron_expr"`
				Kind       string    `json:"kind"`
				Model      string    `json:"model"`
				Instance   string    `json:"instance"`
				Prompt     string    `json:"prompt"`
				Enabled    bool      `json:"enabled"`
				WebhookURL string    `json:"webhook_url"`
				LastRun    time.Time `json:"last_run"`
				NextRun    time.Time `json:"next_run"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(out, &rec); err != nil {
			return err
		}
		sp := rec.Spec
		kind := sp.Kind
		if kind == "" {
			kind = "chat"
		}
		on := "no"
		if sp.Enabled {
			on = "yes"
		}
		fmt.Printf("id:      %s\nname:    %s\nkind:    %s\ncron:    %s\nenabled: %s\n", rec.ID, sp.Name, kind, sp.CronExpr, on)
		if sp.Model != "" {
			fmt.Printf("model:   %s\n", sp.Model)
		}
		if sp.Instance != "" {
			fmt.Printf("instance: %s\n", sp.Instance)
		}
		if sp.Prompt != "" {
			p := sp.Prompt
			if len(p) > 80 {
				p = p[:80] + "…"
			}
			fmt.Printf("prompt:  %s\n", p)
		}
		if sp.WebhookURL != "" {
			fmt.Printf("webhook: %s\n", sp.WebhookURL)
		}
		if !sp.LastRun.IsZero() {
			fmt.Printf("last:    %s\n", sp.LastRun.Local().Format("2006-01-02 15:04:05"))
		}
		if !sp.NextRun.IsZero() {
			fmt.Printf("next:    %s (%s)\n", sp.NextRun.Local().Format("2006-01-02 15:04:05"), untilStr(sp.NextRun))
		}
		return nil
	case "runs":
		fs := flag.NewFlagSet("scheduler runs", flag.ExitOnError)
		n := fs.Int("n", 20, "number of recent runs")
		jsonOut := fs.Bool("json", false, "print the run log as JSON (for scripts)")
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
		if *jsonOut {
			if len(runs) > *n {
				runs = runs[:*n]
			}
			if len(runs) == 0 {
				fmt.Println("[]")
				return nil
			}
			b, err := json.Marshal(runs)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
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
// parseTimeArg parses a time filter value: a Go duration ("30m", "2h") means
// that long ago; anything else must be RFC3339. Returns an RFC3339 UTC string.
func parseTimeArg(v string) (string, error) {
	if d, err := time.ParseDuration(v); err == nil {
		return time.Now().Add(-d).UTC().Format(time.RFC3339), nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", fmt.Errorf("want a duration (30m, 2h) or RFC3339 time")
	}
	return t.UTC().Format(time.RFC3339), nil
}

func cmdRequests(args []string) error {
	fs := flag.NewFlagSet("requests", flag.ExitOnError)
	model := fs.String("model", "", "filter by model id")
	status := fs.String("status", "", "status class (4, 5) or exact code (404)")
	minMs := fs.Int("min-ms", 0, "minimum latency in ms")
	key := fs.String("key", "", "filter by API key prefix")
	instance := fs.String("instance", "", "filter by serving instance id")
	since := fs.String("since", "", "only requests after this time: RFC3339 or a duration like 30m/2h (F189)")
	until := fs.String("until", "", "only requests before this time: RFC3339 or a duration like 30m/2h (F189)")
	id := fs.String("id", "", "show the single request with this id")
	n := fs.Int("n", 20, "number of entries")
	watch := fs.Int("watch", 0, "poll every N seconds and print new requests until Ctrl-C (0 = print once)")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	stats := fs.Bool("stats", false, "print aggregate stats (count, errors, latency percentiles, tokens, cost) instead of the table")
	by := fs.String("by", "", "with -stats: group by model, key or instance")
	jsonOut := fs.Bool("json", false, "print the raw requests JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	if *by != "" && *by != "model" && *by != "key" && *by != "instance" {
		return fmt.Errorf("-by must be model, key or instance")
	}
	if *by != "" && !*stats {
		return fmt.Errorf("-by requires -stats")
	}
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
	if *since != "" {
		if t, err := parseTimeArg(*since); err != nil {
			return fmt.Errorf("invalid -since %q: %v", *since, err)
		} else {
			q.Set("since", t)
		}
	}
	if *until != "" {
		if t, err := parseTimeArg(*until); err != nil {
			return fmt.Errorf("invalid -until %q: %v", *until, err)
		} else {
			q.Set("until", t)
		}
	}
	code, out, err := cc.Get("/api/v1/requests?" + q.Encode())
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("requests returned %d: %s", code, out)
	}
	if *jsonOut && !*stats {
		fmt.Print(string(out))
		return nil
	}
	type reqRow struct {
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
	var entries []reqRow
	if err := json.Unmarshal(out, &entries); err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("no requests yet")
		if *watch <= 0 {
			return nil
		}
	}
	if *stats {
		// statsOf computes aggregates over one slice of entries (F227/F235).
		type statsAgg struct {
			Requests int     `json:"requests"`
			Errors   int     `json:"errors"`
			ErrorPct float64 `json:"errorPct"`
			AvgMs    float64 `json:"avgMs"`
			P50Ms    float64 `json:"p50Ms"`
			P95Ms    float64 `json:"p95Ms"`
			MaxMs    float64 `json:"maxMs"`
			In       int64   `json:"promptTokens"`
			Out      int64   `json:"completionTokens"`
			Cost     float64 `json:"costUSD"`
		}
		statsOf := func(es []reqRow) statsAgg {
			var a statsAgg
			lats := make([]float64, 0, len(es))
			for _, e := range es {
				if e.Status >= 400 {
					a.Errors++
				}
				lats = append(lats, e.LatencyMs)
				a.In += e.PromptTok
				a.Out += e.CompTok
				a.Cost += e.Cost
			}
			a.Requests = len(es)
			sort.Float64s(lats)
			pct := func(p float64) float64 {
				if len(lats) == 0 {
					return 0
				}
				return lats[int(p*float64(len(lats)-1))]
			}
			for _, l := range lats {
				a.AvgMs += l
			}
			if len(lats) > 0 {
				a.AvgMs /= float64(len(lats))
			}
			a.P50Ms, a.P95Ms, a.MaxMs = pct(0.5), pct(0.95), pct(1.0)
			if a.Requests > 0 {
				a.ErrorPct = 100 * float64(a.Errors) / float64(a.Requests)
			}
			return a
		}
		// -by model|key|instance: one row per group (F235).
		if *by != "" {
			groups := map[string][]reqRow{}
			for _, e := range entries {
				g := e.Model
				switch *by {
				case "key":
					g = e.Key
				case "instance":
					g = e.Instance
				}
				if g == "" {
					g = "-"
				}
				groups[g] = append(groups[g], e)
			}
			names := make([]string, 0, len(groups))
			for g := range groups {
				names = append(names, g)
			}
			sort.Strings(names)
			if *jsonOut {
				out2 := map[string]any{}
				for _, g := range names {
					out2[g] = statsOf(groups[g])
				}
				b, _ := json.Marshal(map[string]any{"by": *by, "groups": out2})
				fmt.Println(string(b))
				return nil
			}
			fmt.Printf("%-20s %7s %7s %8s %8s %10s %10s %9s\n", strings.ToUpper(*by), "REQS", "ERRORS", "P50 MS", "P95 MS", "IN", "OUT", "COST $")
			for _, g := range names {
				a := statsOf(groups[g])
				fmt.Printf("%-20s %7d %4d %3.0f%% %8.0f %8.0f %10d %10d %9.4f\n", g, a.Requests, a.Errors, a.ErrorPct, a.P50Ms, a.P95Ms, a.In, a.Out, a.Cost)
			}
			return nil
		}
		a := statsOf(entries)
		if *jsonOut {
			b, _ := json.Marshal(a)
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("requests:  %d (%d errors, %.1f%%)\n", a.Requests, a.Errors, a.ErrorPct)
		fmt.Printf("latency:   avg %.0f ms  p50 %.0f  p95 %.0f  max %.0f\n", a.AvgMs, a.P50Ms, a.P95Ms, a.MaxMs)
		fmt.Printf("tokens:    %d in, %d out\n", a.In, a.Out)
		if a.Cost > 0 {
			fmt.Printf("cost:      $%.4f\n", a.Cost)
		}
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
	printRow := func(e reqRow) {
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
	for _, e := range entries {
		printRow(e)
	}
	if *watch <= 0 {
		return nil
	}
	// Live tail: remember shown ids and print only new requests (F222).
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.ID] = true
	}
	fmt.Printf("\nwatching for new requests every %ds (Ctrl-C to stop)…\n", *watch)
	for {
		time.Sleep(time.Duration(*watch) * time.Second)
		_, cur, err := cc.Get("/api/v1/requests?" + q.Encode())
		if err != nil {
			return err
		}
		var fresh []reqRow
		var all []reqRow
		if json.Unmarshal(cur, &all) != nil {
			continue
		}
		for _, e := range all { // newest-first; collect unseen
			if e.ID != "" && !seen[e.ID] {
				fresh = append(fresh, e)
				seen[e.ID] = true
			}
		}
		for i := len(fresh) - 1; i >= 0; i-- { // print oldest-first
			printRow(fresh[i])
		}
	}
}

// cmdBackups lists, creates or deletes state snapshots in ~/.llmctl/backups.
func cmdBackups(args []string) error {
	fs := flag.NewFlagSet("backups", flag.ExitOnError)
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
		jsonOut := fs.Bool("json", false, "print the raw backups JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/backups")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backups list returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
		fmt.Printf("%-34s %10s  %s\n", "NAME", "SIZE", "CREATED")
		var total int64
		for _, b := range bs {
			total += b.Size
			fmt.Printf("%-34s %10s  %s\n", b.Name, fmtSize(b.Size), b.Created)
		}
		fmt.Printf("%-34s %10s\n", "total: "+strconv.Itoa(len(bs))+" snapshot(s)", fmtSize(total))
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
	case "verify":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl backups verify <name>")
		}
		code, out, err := cc.Post("/api/v1/backups/"+url.PathEscape(rest[0])+"/verify", map[string]any{})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backup verify returned %d: %s", code, out)
		}
		var r struct {
			OK      bool   `json:"ok"`
			Entries int    `json:"entries"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			return err
		}
		if !r.OK {
			return fmt.Errorf("backup %s: %s", rest[0], r.Error)
		}
		fmt.Printf("backup %s ok — %d entries, state.json valid\n", rest[0], r.Entries)
		return nil
	default:
		return fmt.Errorf("unknown backups subcommand %q (list|create|restore|verify|delete)", sub)
	}
}

// cmdMetrics prints the live gateway metrics snapshot: totals plus a
// per-model table (requests, errors, tokens, latency p50/p95, tok/s).
func cmdMetrics(args []string) error {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	watch := fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once)")
	jsonOut := fs.Bool("json", false, "print the raw metrics JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	printOnce := func() error {
		code, out, err := cc.Get("/api/v1/metrics")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("metrics returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		return printMetrics(out)
	}
	if *watch <= 0 {
		return printOnce()
	}
	// Live view: clear + reprint every interval until interrupted (F179).
	for {
		code, out, err := cc.Get("/api/v1/metrics")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("metrics returned %d: %s", code, out)
		}
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live metrics — refreshing every %ds (Ctrl-C to stop)\n\n", *watch)
		if err := printMetrics(out); err != nil {
			return err
		}
		time.Sleep(time.Duration(*watch) * time.Second)
	}
}

func printMetrics(out []byte) error {
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
	modelFlag := fs.String("model", "", "in per-model mode, show only this model id")
	keysFlag := fs.Bool("keys", false, "show daily usage for all keys instead of per-model")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	jsonOut := fs.Bool("json", false, "print the raw usage JSON (for scripts)")
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
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
		if *csvOut { // same columns as `usage -keys -csv` (F260)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"date", "key_id", "requests", "errors", "prompt_tokens", "completion_tokens", "cost_usd"})
			for _, day := range days {
				ids := make([]string, 0, len(kd[day]))
				for id := range kd[day] {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					u := kd[day][id]
					_ = w.Write([]string{day, id, strconv.FormatInt(u.Requests, 10), strconv.FormatInt(u.Errors, 10),
						strconv.FormatInt(u.Prompt, 10), strconv.FormatInt(u.Comp, 10), strconv.FormatFloat(u.Cost, 'f', 6, 64)})
				}
			}
			w.Flush()
			return w.Error()
		}
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
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
	if *jsonOut && *modelFlag == "" {
		fmt.Print(string(out))
		return nil
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
	// -model filter: keep only that model's rows (F230).
	if *modelFlag != "" {
		for day, models := range daily {
			for m := range models {
				if m != *modelFlag {
					delete(models, m)
				}
			}
			if len(models) == 0 {
				delete(daily, day)
			}
		}
		if *jsonOut {
			b, err := json.Marshal(daily)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
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
	// Window totals (F209).
	var tReq, tErr, tIn, tOut int64
	var tCost float64
	for _, models := range daily {
		for _, u := range models {
			tReq += u.Requests
			tErr += u.Errors
			tIn += u.Prompt
			tOut += u.Comp
			tCost += u.Cost
		}
	}
	fmt.Printf("%s  %6d reqs %4d err %10d in %10d out", "total", tReq, tErr, tIn, tOut)
	if tCost > 0 {
		fmt.Printf("  $%.4f", tCost)
	}
	fmt.Println()
	return nil
}

// cmdEvents prints the recent daemon events feed (newest first).
func cmdEvents(args []string) error {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	n := fs.Int("n", 20, "number of events")
	kind := fs.String("kind", "", "only this event kind, e.g. instance.crashed")
	watch := fs.Int("watch", 0, "poll every N seconds and print new events until Ctrl-C (0 = print once)")
	jsonOut := fs.Bool("json", false, "print the raw events JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	u := "/api/v1/events?limit=" + strconv.Itoa(*n)
	if *kind != "" {
		u += "&kind=" + url.QueryEscape(*kind)
	}
	if *jsonOut {
		code, out, err := cc.Get(u)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("events returned %d: %s", code, out)
		}
		fmt.Print(string(out))
		return nil
	}
	fetch := func() ([]eventRow, error) {
		code, out, err := cc.Get(u)
		if err != nil {
			return nil, err
		}
		if code != 200 {
			return nil, fmt.Errorf("events returned %d: %s", code, out)
		}
		var evs []eventRow
		if err := json.Unmarshal(out, &evs); err != nil {
			return nil, err
		}
		return evs, nil
	}
	printRow := func(e eventRow) {
		fmt.Printf("%-19s %-26s %-16s %s\n",
			e.Time.Local().Format("2006-01-02 15:04:05"), e.Kind, e.Instance, e.Model)
	}
	evs, err := fetch()
	if err != nil {
		return err
	}
	if *csvOut { // F261
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"time", "kind", "instance", "model"})
		for _, e := range evs {
			_ = w.Write([]string{e.Time.UTC().Format(time.RFC3339), e.Kind, e.Instance, e.Model})
		}
		w.Flush()
		return w.Error()
	}
	if len(evs) == 0 {
		fmt.Println("no events since daemon start")
	} else {
		fmt.Printf("%-19s %-26s %-16s %s\n", "TIME", "EVENT", "INSTANCE", "MODEL")
		for _, e := range evs {
			printRow(e)
		}
	}
	if *watch <= 0 {
		return nil
	}
	// Live tail: remember what we've shown and print only newer events (F187).
	seen := map[string]bool{}
	key := func(e eventRow) string {
		return fmt.Sprintf("%d|%s|%s|%s", e.Time.UnixNano(), e.Kind, e.Instance, e.Model)
	}
	for _, e := range evs {
		seen[key(e)] = true
	}
	fmt.Printf("\nwatching for new events every %ds (Ctrl-C to stop)…\n", *watch)
	for {
		time.Sleep(time.Duration(*watch) * time.Second)
		cur, err := fetch()
		if err != nil {
			return err
		}
		var fresh []eventRow
		for _, e := range cur { // newest-first; collect unseen
			if !seen[key(e)] {
				fresh = append(fresh, e)
				seen[key(e)] = true
			}
		}
		for i := len(fresh) - 1; i >= 0; i-- { // print oldest-first
			printRow(fresh[i])
		}
	}
}

// eventRow is one entry of the daemon events feed.
type eventRow struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
	Instance string    `json:"instance"`
	Model    string    `json:"model"`
}

// cmdWebhooks shows recent webhook delivery attempts (F191), backed by
// GET /api/v1/webhook/log.
func cmdWebhooks(args []string) error {
	fs := flag.NewFlagSet("webhooks", flag.ExitOnError)
	n := fs.Int("n", 20, "number of deliveries")
	jsonOut := fs.Bool("json", false, "print the delivery log as JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/webhook/log")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("webhook log returned %d", code)
	}
	var list []struct {
		Time   time.Time `json:"time"`
		URL    string    `json:"url"`
		Event  string    `json:"event"`
		Status int       `json:"status"`
		OK     bool      `json:"ok"`
		Error  string    `json:"error"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	if len(list) > *n {
		list = list[:*n]
	}
	if *jsonOut {
		b, err := json.Marshal(list)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if len(list) == 0 {
		fmt.Println("no webhook deliveries yet")
		return nil
	}
	fmt.Printf("%-14s %-24s %-6s %-5s %s\n", "TIME", "EVENT", "STATUS", "OK", "URL / ERROR")
	for _, d := range list {
		status := "-"
		if d.Status > 0 {
			status = strconv.Itoa(d.Status)
		}
		ok := "no"
		if d.OK {
			ok = "yes"
		}
		info := d.URL
		if d.Error != "" {
			info += " — " + d.Error
		}
		ev := d.Event
		if ev == "" {
			ev = "-"
		}
		fmt.Printf("%-14s %-24s %-6s %-5s %s\n",
			d.Time.Local().Format("01-02 15:04:05"), ev, status, ok, info)
	}
	return nil
}

// cmdChats lists saved chat conversations (F244).
func cmdChats(args []string) error {
	if len(args) > 0 && args[0] == "show" {
		sfs := flag.NewFlagSet("chats show", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw chat JSON (for scripts)")
		sfs.Parse(reorderFlags(args[1:]))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats show <id> [-json]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat %s: %s", sfs.Arg(0), out)
		}
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		var ch struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Model     string `json:"model"`
			UpdatedAt string `json:"updatedAt"`
			Context   struct {
				UsedTokens int `json:"usedTokens"`
			} `json:"context"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(out, &ch); err != nil {
			return err
		}
		fmt.Printf("id:      %s\ntitle:   %s\nmodel:   %s\nupdated: %s\ntokens:  %d\n\n",
			ch.ID, ch.Title, ch.Model, ch.UpdatedAt, ch.Context.UsedTokens)
		for _, m := range ch.Messages {
			c := strings.TrimSpace(m.Content)
			if len(c) > 100 {
				c = c[:100] + "…"
			}
			fmt.Printf("[%s] %s\n", m.Role, strings.ReplaceAll(c, "\n", " "))
		}
		return nil
	}
	fs := flag.NewFlagSet("chats", flag.ExitOnError)
	n := fs.Int("n", 20, "number of conversations")
	jsonOut := fs.Bool("json", false, "print the chat list as JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/chats")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("chats returned %d: %s", code, out)
	}
	filter := ""
	if fs.NArg() > 0 {
		filter = strings.ToLower(fs.Arg(0))
	}
	var list []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Model     string `json:"model"`
		Messages  int    `json:"messages"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	if filter != "" {
		var keep []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Model     string `json:"model"`
			Messages  int    `json:"messages"`
			UpdatedAt string `json:"updatedAt"`
		}
		for _, c := range list {
			if strings.Contains(strings.ToLower(c.Title), filter) || strings.Contains(strings.ToLower(c.Model), filter) {
				keep = append(keep, c)
			}
		}
		list = keep
	}
	if len(list) > *n {
		list = list[:*n]
	}
	if *jsonOut {
		b, err := json.Marshal(list)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if len(list) == 0 {
		fmt.Println("no saved chats")
		return nil
	}
	fmt.Printf("%-18s %-14s %-4s %-14s %s\n", "ID", "MODEL", "MSGS", "UPDATED", "TITLE")
	for _, c := range list {
		updated := c.UpdatedAt
		if t, err := time.Parse(time.RFC3339, c.UpdatedAt); err == nil {
			updated = t.Local().Format("01-02 15:04")
		}
		title := c.Title
		if len(title) > 48 {
			title = title[:48] + "…"
		}
		fmt.Printf("%-18s %-14s %-4d %-14s %s\n", c.ID, c.Model, c.Messages, updated, title)
	}
	return nil
}

// cmdSkills lists and manages skill packs (F245).
func cmdSkills(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	sub, rest := "list", args
	if len(args) > 0 {
		switch args[0] {
		case "list", "show", "enable", "disable", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("skills", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the skills as JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/skills")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skills returned %d: %s", code, out)
		}
		filter := ""
		if fs.NArg() > 0 {
			filter = strings.ToLower(fs.Arg(0))
		}
		var list []struct {
			ID       string `json:"id"`
			Manifest struct {
				ID          string   `json:"id"`
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Tags        []string `json:"tags"`
				Enabled     bool     `json:"enabled"`
				PromptTmpl  string   `json:"prompt_template"`
			} `json:"manifest"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if filter != "" {
			var keep []struct {
				ID       string `json:"id"`
				Manifest struct {
					ID          string   `json:"id"`
					Name        string   `json:"name"`
					Description string   `json:"description"`
					Tags        []string `json:"tags"`
					Enabled     bool     `json:"enabled"`
					PromptTmpl  string   `json:"prompt_template"`
				} `json:"manifest"`
			}
			for _, s := range list {
				hay := strings.ToLower(s.Manifest.ID + " " + s.Manifest.Name + " " + s.Manifest.Description + " " + strings.Join(s.Manifest.Tags, " "))
				if strings.Contains(hay, filter) {
					keep = append(keep, s)
				}
			}
			list = keep
		}
		if *jsonOut {
			b, err := json.Marshal(list)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		if len(list) == 0 {
			fmt.Println("no skills installed")
			return nil
		}
		fmt.Printf("%-20s %-6s %-20s %s\n", "ID", "ON", "NAME", "DESCRIPTION")
		for _, s := range list {
			on := "off"
			if s.Manifest.Enabled {
				on = "on "
			}
			desc := s.Manifest.Description
			if len(desc) > 48 {
				desc = desc[:48] + "…"
			}
			fmt.Printf("%-20s %-6s %-20s %s\n", s.Manifest.ID, on, s.Manifest.Name, desc)
		}
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl skills show <id>")
		}
		code, out, err := cc.Get("/api/v1/skills/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skill %s: %s", rest[0], out)
		}
		fmt.Print(string(out))
		return nil
	case "enable", "disable":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl skills %s <id>", sub)
		}
		body := map[string]any{"enabled": sub == "enable"}
		code, out, err := cc.Post("/api/v1/skills/"+url.PathEscape(rest[0])+"/enabled", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s returned %d: %s", sub, code, out)
		}
		fmt.Printf("%sd skill %s\n", sub, rest[0])
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl skills delete <id>")
		}
		code, out, err := cc.Delete("/api/v1/skills/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		fmt.Printf("deleted skill %s\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown skills subcommand %q (list|show|enable|disable|delete)", sub)
	}
}

// cmdMemory lists and manages memory entries (F246).
func cmdMemory(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	sub, rest := "list", args
	if len(args) > 0 {
		switch args[0] {
		case "list", "show", "add", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("memory", flag.ExitOnError)
		n := fs.Int("n", 30, "number of entries")
		jsonOut := fs.Bool("json", false, "print the entries as JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/memory")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory returned %d: %s", code, out)
		}
		filter := ""
		if fs.NArg() > 0 {
			filter = strings.ToLower(fs.Arg(0))
		}
		var list []struct {
			Entry struct {
				ID        string   `json:"id"`
				Content   string   `json:"content"`
				Tags      []string `json:"tags"`
				CreatedAt string   `json:"created_at"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if filter != "" {
			var keep []struct {
				Entry struct {
					ID        string   `json:"id"`
					Content   string   `json:"content"`
					Tags      []string `json:"tags"`
					CreatedAt string   `json:"created_at"`
				} `json:"entry"`
			}
			for _, m := range list {
				hay := strings.ToLower(m.Entry.Content + " " + strings.Join(m.Entry.Tags, " "))
				if strings.Contains(hay, filter) {
					keep = append(keep, m)
				}
			}
			list = keep
		}
		if len(list) > *n {
			list = list[:*n]
		}
		if *jsonOut {
			b, err := json.Marshal(list)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		if len(list) == 0 {
			fmt.Println("no memory entries")
			return nil
		}
		for _, m := range list {
			content := strings.ReplaceAll(strings.TrimSpace(m.Entry.Content), "\n", " ")
			if len(content) > 80 {
				content = content[:80] + "…"
			}
			fmt.Printf("%-16s %s", m.Entry.ID, content)
			if len(m.Entry.Tags) > 0 {
				fmt.Printf("  [%s]", strings.Join(m.Entry.Tags, ","))
			}
			fmt.Println()
		}
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl memory show <id>")
		}
		code, out, err := cc.Get("/api/v1/memory/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory %s: %s", rest[0], out)
		}
		fmt.Print(string(out))
		return nil
	case "add":
		afs := flag.NewFlagSet("memory add", flag.ExitOnError)
		tags := afs.String("tags", "", "comma-separated tags")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory add \"<text>\" [-tags a,b]")
		}
		body := map[string]any{"content": strings.Join(afs.Args(), " ")}
		if *tags != "" {
			list := []string{}
			for _, t := range strings.Split(*tags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					list = append(list, t)
				}
			}
			body["tags"] = list
		}
		code, out, err := cc.Post("/api/v1/memory", body)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("memory add returned %d: %s", code, out)
		}
		var rec struct {
			Entry struct {
				ID string `json:"id"`
			} `json:"entry"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("added memory %s\n", rec.Entry.ID)
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl memory delete <id>")
		}
		code, out, err := cc.Delete("/api/v1/memory/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		fmt.Printf("deleted memory %s\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown memory subcommand %q (list|show|add|delete)", sub)
	}
}

// cmdKb lists and manages knowledge bases (F247).
func cmdKb(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	sub, rest := "list", args
	if len(args) > 0 {
		switch args[0] {
		case "list", "show", "create", "docs", "add-doc", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("kb", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the knowledge bases as JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/knowledge-bases")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb returned %d: %s", code, out)
		}
		filter := ""
		if fs.NArg() > 0 {
			filter = strings.ToLower(fs.Arg(0))
		}
		var list []struct {
			KB struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				DocCount    int    `json:"doc_count"`
			} `json:"kb"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if filter != "" {
			var keep []struct {
				KB struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Description string `json:"description"`
					DocCount    int    `json:"doc_count"`
				} `json:"kb"`
			}
			for _, kb := range list {
				if strings.Contains(strings.ToLower(kb.KB.Name+" "+kb.KB.Description), filter) {
					keep = append(keep, kb)
				}
			}
			list = keep
		}
		if *jsonOut {
			b, err := json.Marshal(list)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		if len(list) == 0 {
			fmt.Println("no knowledge bases")
			return nil
		}
		fmt.Printf("%-20s %-24s %-5s %s\n", "ID", "NAME", "DOCS", "DESCRIPTION")
		for _, kb := range list {
			desc := kb.KB.Description
			if len(desc) > 40 {
				desc = desc[:40] + "…"
			}
			fmt.Printf("%-20s %-24s %-5d %s\n", kb.KB.ID, kb.KB.Name, kb.KB.DocCount, desc)
		}
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl kb show <id>")
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb %s: %s", rest[0], out)
		}
		fmt.Print(string(out))
		return nil
	case "docs":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl kb docs <id>")
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(rest[0]) + "/documents")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb docs returned %d: %s", code, out)
		}
		var docs []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}
		if err := json.Unmarshal(out, &docs); err != nil {
			return err
		}
		if len(docs) == 0 {
			fmt.Println("no documents")
			return nil
		}
		fmt.Printf("%-18s %-40s %s\n", "ID", "FILENAME", "BYTES")
		for _, d := range docs {
			fmt.Printf("%-18s %-40s %d\n", d.ID, d.Filename, d.Size)
		}
		return nil
	case "add-doc":
		dfs := flag.NewFlagSet("kb add-doc", flag.ExitOnError)
		name := dfs.String("name", "", "store under this filename (default: the file's base name)")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl kb add-doc <kb-id> <path> [-name X]")
		}
		f, err := os.Open(dfs.Arg(1))
		if err != nil {
			return err
		}
		defer f.Close()
		storeName := filepath.Base(dfs.Arg(1))
		if *name != "" {
			storeName = *name
		}
		code, out, err := multipartPost(cc, "/api/v1/knowledge-bases/"+url.PathEscape(dfs.Arg(0))+"/documents", storeName, f)
		if err != nil {
			return err
		}
		if code != 201 && code != 200 {
			return fmt.Errorf("add-doc returned %d: %s", code, out)
		}
		var rec struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}
		if err := json.Unmarshal(out, &rec); err != nil {
			return err
		}
		fmt.Printf("added %s (%d bytes) as %s\n", rec.Filename, rec.Size, rec.ID)
		return nil
	case "create":
		cfs := flag.NewFlagSet("kb create", flag.ExitOnError)
		desc := cfs.String("desc", "", "description")
		cfs.Parse(reorderFlags(rest))
		if cfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb create <name> [-desc X]")
		}
		code, out, err := cc.Post("/api/v1/knowledge-bases", map[string]any{"name": cfs.Arg(0), "description": *desc})
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("kb create returned %d: %s", code, out)
		}
		var rec struct {
			KB struct {
				ID string `json:"id"`
			} `json:"kb"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("created knowledge base %s\n", rec.KB.ID)
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl kb delete <id>")
		}
		code, out, err := cc.Delete("/api/v1/knowledge-bases/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		fmt.Printf("deleted knowledge base %s\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown kb subcommand %q (list|show|create|docs|add-doc|delete)", sub)
	}
}

// multipartPost posts a file as multipart/form-data field "file" to path and
// returns the response body (F250).
func multipartPost(cc *daemon.ControlClient, path, filename string, r io.Reader) (int, []byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return 0, nil, err
	}
	if _, err := io.Copy(part, r); err != nil {
		return 0, nil, err
	}
	mw.Close()
	req, err := http.NewRequest("POST", cc.Base+path, &buf)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

// cmdTools lists and manages installed tools (F248).
func cmdTools(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	sub, rest := "list", args
	if len(args) > 0 {
		switch args[0] {
		case "list", "show", "install", "enable", "disable", "run", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("tools", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the tools as JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/tools")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("tools returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID       string `json:"id"`
			Manifest struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Runtime     string `json:"runtime"`
			} `json:"manifest"`
			Enabled bool `json:"enabled"`
			Sandbox bool `json:"sandbox"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no tools installed")
			return nil
		}
		fmt.Printf("%-20s %-4s %-8s %-9s %s\n", "ID", "ON", "SANDBOX", "RUNTIME", "DESCRIPTION")
		for _, t := range list {
			on := "off"
			if t.Enabled {
				on = "on "
			}
			sb := "off"
			if t.Sandbox {
				sb = "on"
			}
			rt := t.Manifest.Runtime
			if rt == "" {
				rt = "python"
			}
			fmt.Printf("%-20s %-4s %-8s %-9s %s\n", t.ID, on, sb, rt, t.Manifest.Description)
		}
		return nil
	case "install":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl tools install <dir>")
		}
		abs, err := filepath.Abs(rest[0])
		if err != nil {
			return err
		}
		code, out, err := cc.Post("/api/v1/tools/install", map[string]any{"dir": abs})
		if err != nil {
			return err
		}
		if code != 201 && code != 200 {
			return fmt.Errorf("install returned %d: %s", code, out)
		}
		var rec struct {
			ID string `json:"id"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("installed tool %s\n", rec.ID)
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl tools show <id>")
		}
		code, out, err := cc.Get("/api/v1/tools")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("tools returned %d: %s", code, out)
		}
		var list []map[string]any
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		for _, t := range list {
			if t["id"] == rest[0] {
				b, _ := json.MarshalIndent(t, "", "  ")
				fmt.Println(string(b))
				return nil
			}
		}
		return fmt.Errorf("no such tool: %s", rest[0])
	case "enable", "disable":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl tools %s <id>", sub)
		}
		body := map[string]any{"enabled": sub == "enable"}
		code, out, err := cc.Post("/api/v1/tools/"+url.PathEscape(rest[0])+"/enabled", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s returned %d: %s", sub, code, out)
		}
		fmt.Printf("%sd tool %s\n", sub, rest[0])
		return nil
	case "run":
		rfs := flag.NewFlagSet("tools run", flag.ExitOnError)
		argsJSON := rfs.String("args", "{}", "JSON object of tool arguments")
		timeout := rfs.Int("timeout", 0, "timeout in seconds (0 = default)")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl tools run <id> [-args JSON] [-timeout N]")
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(*argsJSON), &parsed); err != nil {
			return fmt.Errorf("-args must be a JSON object: %w", err)
		}
		body := map[string]any{"args": parsed}
		if *timeout > 0 {
			body["timeoutSec"] = *timeout
		}
		code, out, err := cc.Post("/api/v1/tools/"+url.PathEscape(rfs.Arg(0))+"/run", body)
		if err != nil {
			return err
		}
		var res struct {
			OK       bool   `json:"ok"`
			Result   string `json:"result"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exitCode"`
			Error    string `json:"error"`
			Duration string `json:"duration"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return fmt.Errorf("run returned %d: %s", code, out)
		}
		if res.Error != "" {
			return fmt.Errorf("tool run failed: %s", res.Error)
		}
		fmt.Print(res.Result)
		if res.Stderr != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
		if !res.OK {
			return fmt.Errorf("tool exited %d", res.ExitCode)
		}
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl tools delete <id>")
		}
		code, out, err := cc.Delete("/api/v1/tools/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		fmt.Printf("deleted tool %s\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown tools subcommand %q (list|show|install|enable|disable|run|delete)", sub)
	}
}

// cmdMcp lists and manages MCP servers (F249).
func cmdMcp(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	sub, rest := "list", args
	if len(args) > 0 {
		switch args[0] {
		case "list", "add", "remove", "enable", "disable", "test":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("mcp", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the servers as JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/mcp")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("mcp returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Enabled bool     `json:"enabled"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no MCP servers configured")
			return nil
		}
		fmt.Printf("%-18s %-20s %-6s %s\n", "ID", "NAME", "ON", "COMMAND")
		for _, s := range list {
			on := "off"
			if s.Enabled {
				on = "on "
			}
			cmd := s.Command
			if len(s.Args) > 0 {
				cmd += " " + strings.Join(s.Args, " ")
			}
			fmt.Printf("%-18s %-20s %-6s %s\n", s.ID, s.Name, on, cmd)
		}
		return nil
	case "add":
		afs := flag.NewFlagSet("mcp add", flag.ExitOnError)
		sargs := afs.String("args", "", "space-separated server arguments")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl mcp add <name> <command> [-args 'a b']")
		}
		body := map[string]any{"name": afs.Arg(0), "command": afs.Arg(1)}
		if *sargs != "" {
			var list []string
			for _, a := range strings.Fields(*sargs) {
				list = append(list, a)
			}
			body["args"] = list
		}
		code, out, err := cc.Post("/api/v1/mcp", body)
		if err != nil {
			return err
		}
		if code != 201 && code != 200 {
			return fmt.Errorf("mcp add returned %d: %s", code, out)
		}
		var rec struct {
			ID string `json:"id"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("added MCP server %s\n", rec.ID)
		return nil
	case "remove":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl mcp remove <id>")
		}
		code, out, err := cc.Delete("/api/v1/mcp/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("remove returned %d: %s", code, out)
		}
		fmt.Printf("removed MCP server %s\n", rest[0])
		return nil
	case "enable", "disable":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl mcp %s <id>", sub)
		}
		body := map[string]any{"enabled": sub == "enable"}
		code, out, err := cc.Post("/api/v1/mcp/"+url.PathEscape(rest[0])+"/enabled", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s returned %d: %s", sub, code, out)
		}
		fmt.Printf("%sd MCP server %s\n", sub, rest[0])
		return nil
	case "test":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl mcp test <id>")
		}
		code, out, err := cc.Post("/api/v1/mcp/"+url.PathEscape(rest[0])+"/test", map[string]any{})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("server unreachable: %s", strings.TrimSpace(string(out)))
		}
		var res struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		fmt.Printf("ok — %d tool(s)\n", len(res.Tools))
		for _, t := range res.Tools {
			fmt.Printf("  %-20s %s\n", t.Name, t.Description)
		}
		return nil
	default:
		return fmt.Errorf("unknown mcp subcommand %q (list|add|remove|enable|disable|test)", sub)
	}
}

// cmdCost prints token/cost totals from the gateway counters (F251).
func cmdCost(args []string) error {
	fs := flag.NewFlagSet("cost", flag.ExitOnError)
	byKey := fs.Bool("keys", false, "show per-key costs instead of per-model")
	jsonOut := fs.Bool("json", false, "print the raw cost JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, sum, err := cc.Get("/api/v1/cost")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("cost returned %d: %s", code, sum)
	}
	var totals struct {
		CostUSD      float64 `json:"cost_usd"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		TotalTokens  int64   `json:"total_tokens"`
	}
	if err := json.Unmarshal(sum, &totals); err != nil {
		return err
	}
	groupPath := "/api/v1/cost/models"
	if *byKey {
		groupPath = "/api/v1/cost/keys"
	}
	code, groups, err := cc.Get(groupPath)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("cost %s returned %d: %s", groupPath, code, groups)
	}
	if *jsonOut {
		var payload struct {
			Totals json.RawMessage `json:"totals"`
			Groups json.RawMessage `json:"groups"`
		}
		payload.Totals = json.RawMessage(sum)
		payload.Groups = json.RawMessage(groups)
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("total: $%.4f — %d tokens (in %d / out %d)\n\n",
		totals.CostUSD, totals.TotalTokens, totals.InputTokens, totals.OutputTokens)
	var usage map[string]struct {
		InputTok  int64   `json:"input_tokens"`
		OutputTok int64   `json:"output_tokens"`
		TotalTok  int64   `json:"total_tokens"`
		Requests  int64   `json:"requests"`
		Cost      float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal(groups, &usage); err != nil {
		return err
	}
	if len(usage) == 0 {
		fmt.Println("no cost recorded yet")
		return nil
	}
	// Optional friendly names for key ids (F251).
	names := map[string]string{}
	if *byKey {
		if code, kout, err := cc.Get("/api/v1/keys"); err == nil && code == 200 {
			var keys []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if json.Unmarshal(kout, &keys) == nil {
				for _, k := range keys {
					names[k.ID] = k.Name
				}
			}
		}
	}
	type row struct {
		id string
		u  struct {
			InputTok  int64   `json:"input_tokens"`
			OutputTok int64   `json:"output_tokens"`
			TotalTok  int64   `json:"total_tokens"`
			Requests  int64   `json:"requests"`
			Cost      float64 `json:"cost_usd"`
		}
	}
	rows := []row{}
	for id, u := range usage {
		r := row{id: id}
		r.u = u
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].u.Cost > rows[j].u.Cost })
	label := "MODEL"
	if *byKey {
		label = "KEY"
	}
	fmt.Printf("%-20s %-28s %-8s %-10s %-9s %s\n", label, "NAME", "REQS", "TOKENS", "IN/OUT", "COST USD")
	for _, r := range rows {
		id := r.id
		if id == "" {
			id = "(unknown)" // requests that failed before model resolution
		}
		name := names[r.id]
		if name == "" {
			name = "-"
		}
		if len(name) > 28 {
			name = name[:28]
		}
		fmt.Printf("%-20s %-28s %-8d %-10d %d/%-8d %.4f\n",
			id, name, r.u.Requests, r.u.TotalTok, r.u.InputTok, r.u.OutputTok, r.u.Cost)
	}
	return nil
}

// cmdPrompts lists the built-in prompt library (F218).
func cmdPrompts(args []string) error {
	if len(args) > 0 && args[0] == "show" {
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl prompts show <id>")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/prompts/" + url.PathEscape(args[1]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompt %q not found (%d)", args[1], code)
		}
		var p struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(out, &p); err != nil {
			return err
		}
		fmt.Printf("%s (%s)\n\n%s\n", p.Name, p.ID, p.Content)
		return nil
	}
	fs := flag.NewFlagSet("prompts", flag.ExitOnError)
	category := fs.String("category", "", "only this category")
	jsonOut := fs.Bool("json", false, "print the prompts as JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	filter := strings.ToLower(fs.Arg(0))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/prompts")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("prompts returned %d: %s", code, out)
	}
	type promptRow struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
	}
	var list []promptRow
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	var shown []promptRow
	for _, p := range list {
		if *category != "" && !strings.EqualFold(p.Category, *category) {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(p.ID+" "+p.Name+" "+p.Description+" "+p.Category), filter) {
			continue
		}
		shown = append(shown, p)
	}
	if *jsonOut {
		if len(shown) == len(list) {
			fmt.Print(string(out))
			return nil
		}
		b, err := json.Marshal(shown)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if len(shown) == 0 {
		fmt.Println("no prompts match")
		return nil
	}
	fmt.Printf("%-16s %-12s %-22s %s\n", "ID", "CATEGORY", "NAME", "DESCRIPTION")
	for _, p := range shown {
		fmt.Printf("%-16s %-12s %-22s %s\n", p.ID, p.Category, p.Name, p.Description)
	}
	return nil
}

// cmdFiles uploads, lists, prints and deletes files (F219, F243).
func cmdFiles(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	sub := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	switch sub {
	case "upload":
		ufs := flag.NewFlagSet("files upload", flag.ExitOnError)
		name := ufs.String("name", "", "store under this filename (default: the file's base name)")
		ujson := ufs.Bool("json", false, "print the raw creation JSON (for scripts)")
		ufs.Parse(reorderFlags(rest))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl files upload <path> [-name X] [-json]")
		}
		path := ufs.Arg(0)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		storeName := filepath.Base(path)
		if *name != "" {
			storeName = *name
		}
		code, out, err := multipartPost(cc, "/api/v1/files", storeName, f)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("upload returned %d: %s", code, out)
		}
		if *ujson {
			fmt.Print(string(out))
			return nil
		}
		var rec struct {
			ID    string `json:"id"`
			Entry struct {
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(out, &rec); err != nil {
			return err
		}
		fmt.Printf("uploaded %s (%d bytes) as %s\n", rec.Entry.Filename, rec.Entry.Size, rec.ID)
		return nil
	case "list":
		fs := flag.NewFlagSet("files", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw files JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/files")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("files returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID    string `json:"id"`
			Entry struct {
				Filename    string    `json:"filename"`
				ContentType string    `json:"contentType"`
				Size        int64     `json:"size"`
				Source      string    `json:"source"`
				CreatedAt   time.Time `json:"created_at"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no uploaded files (upload via the web UI)")
			return nil
		}
		fmt.Printf("%-16s %10s  %-28s %-8s %-17s %s\n", "ID", "SIZE", "TYPE", "SOURCE", "CREATED", "FILENAME")
		for _, f := range list {
			fmt.Printf("%-16s %10s  %-28s %-8s %-17s %s\n", f.ID, fmtSize(f.Entry.Size), f.Entry.ContentType, f.Entry.Source,
				f.Entry.CreatedAt.Local().Format("2006-01-02 15:04:05"), f.Entry.Filename)
		}
		return nil
	case "cat":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl files cat <id>")
		}
		code, out, err := cc.Get("/api/v1/files/" + url.PathEscape(rest[0]) + "/content")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("file %q not found (%d)", rest[0], code)
		}
		fmt.Print(string(out))
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl files delete <id>")
		}
		code, out, err := cc.Delete("/api/v1/files/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		fmt.Printf("deleted %s\n", rest[0])
		return nil
	default:
		return fmt.Errorf("usage: llmctl files [list|cat <id>|delete <id>] [-json]")
	}
}

// cmdSettings shows and edits daemon settings (F220). `settings` prints,
// `settings set -flag value…` PATCHes only the flags given.
func cmdSettings(args []string) error {
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "test-webhook" {
		code, out, err := cc.Post("/api/v1/settings/notify-test", map[string]any{})
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if code != 200 {
			return fmt.Errorf("notify-test returned %d", code)
		}
		return nil
	}
	if len(args) > 0 && args[0] == "set" {
		fs := flag.NewFlagSet("settings set", flag.ExitOnError)
		requireAuth := fs.Bool("require-auth", false, "gateway requires API keys")
		idleMin := fs.Int("idle-min", -1, "idle unload minutes (-1 = leave)")
		notifyWebhook := fs.String("notify-webhook", "", "webhook URL for events")
		notifySecret := fs.String("notify-secret", "", "HMAC secret for the notify webhook")
		allowCORS := fs.Bool("allow-cors", false, "CORS: * on the API")
		startOnDemand := fs.Bool("start-on-demand", false, "start instances on request")
		maxBodyMB := fs.Int("max-body-mb", -1, "max request body MB (-1 = leave)")
		rateRPS := fs.Float64("rate-rps", -1, "gateway default requests/sec (-1 = leave)")
		rateBurst := fs.Int("rate-burst", -1, "gateway default burst (-1 = leave)")
		retentionDays := fs.Int("retention-days", -2, "usage rollup days kept, -1 = forever (-2 = leave)")
		logLevel := fs.String("log-level", "", "daemon log level: debug|info|warn|error")
		backupKeep := fs.Int("backup-keep", -1, "snapshots kept (-1 = leave)")
		fs.Parse(reorderFlags(args[1:]))
		body := map[string]any{}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "require-auth":
				body["requireAuth"] = *requireAuth
			case "idle-min":
				body["idleUnloadMin"] = *idleMin
			case "notify-webhook":
				body["notifyWebhook"] = *notifyWebhook
			case "notify-secret":
				body["notifyWebhookSecret"] = *notifySecret
			case "allow-cors":
				body["allowCORS"] = *allowCORS
			case "start-on-demand":
				body["startOnDemand"] = *startOnDemand
			case "max-body-mb":
				body["maxBodyMB"] = *maxBodyMB
			case "rate-rps":
				body["rateRPS"] = *rateRPS
			case "rate-burst":
				body["rateBurst"] = *rateBurst
			case "retention-days":
				body["usageRetentionDays"] = *retentionDays
			case "log-level":
				body["logLevel"] = *logLevel
			case "backup-keep":
				body["backupKeep"] = *backupKeep
			}
		})
		if len(body) == 0 {
			return fmt.Errorf("nothing to set (pass at least one -flag)")
		}
		code, out, err := cc.Do("PATCH", "/api/v1/settings", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("settings set returned %d: %s", code, out)
		}
		fmt.Println("settings updated")
		return nil
	}
	fs := flag.NewFlagSet("settings", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw settings JSON (for scripts)")
	fs.Parse(reorderFlags(args))
	code, out, err := cc.Get("/api/v1/settings")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("settings returned %d: %s", code, out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		return err
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-22s %v\n", k+":", v[k])
	}
	return nil
}

// cmdBatch runs many prompts through the gateway in one call (F224).
// Prompts come from args, -file (one per line) or piped stdin.
func cmdBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	model := fs.String("model", "", "model id (required)")
	system := fs.String("system", "", "system prompt")
	maxTokens := fs.Int("max-tokens", 0, "max tokens per completion")
	concurrency := fs.Int("concurrency", 4, "parallel requests (max 8)")
	key := fs.String("key", "", "gateway API key (needed when auth is on)")
	file := fs.String("file", "", "read prompts from this file, one per line")
	jsonOut := fs.Bool("json", false, "print the raw batch JSON (for scripts)")
	fs.Parse(reorderFlags(args))

	var prompts []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			prompts = append(prompts, s)
		}
	}
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			add(line)
		}
	} else if fi, _ := os.Stdin.Stat(); fi != nil && fi.Mode()&os.ModeCharDevice == 0 {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			add(sc.Text())
		}
	}
	for _, a := range fs.Args() {
		add(a)
	}
	if *model == "" || len(prompts) == 0 {
		return fmt.Errorf("usage: llmctl batch -model ID [-system S] [-key K] [-file F|piped stdin|prompts…]")
	}
	body := map[string]any{
		"model": *model, "prompts": prompts, "concurrency": *concurrency,
	}
	if *system != "" {
		body["system"] = *system
	}
	if *maxTokens > 0 {
		body["maxTokens"] = *maxTokens
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", cc.Base+"/api/v1/batch", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if *key != "" {
		req.Header.Set("Authorization", "Bearer "+*key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("batch returned %d: %s", resp.StatusCode, out)
	}
	if *jsonOut {
		fmt.Println(string(out))
		return nil
	}
	var res struct {
		OK      int `json:"ok"`
		Failed  int `json:"failed"`
		Results []struct {
			Prompt  string `json:"prompt"`
			Content string `json:"content"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	for _, r := range res.Results {
		p := r.Prompt
		if len(p) > 40 {
			p = p[:40] + "…"
		}
		if r.Error != "" {
			fmt.Printf("✗ %-42s %s\n", p, r.Error)
			continue
		}
		c := strings.TrimSpace(r.Content)
		if len(c) > 120 {
			c = c[:120] + "…"
		}
		fmt.Printf("✓ %-42s %s\n", p, strings.ReplaceAll(c, "\n", " "))
	}
	fmt.Printf("%d ok, %d failed\n", res.OK, res.Failed)
	return nil
}

func cmdKey(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl key <generate|show|note|enable|disable|rotate|list|revoke|prune|reset-usage>")
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
		note := fs.String("note", "", "free-text annotation")
		jsonOut := fs.Bool("json", false, "print the raw creation JSON, including the secret (for scripts)")
		fs.Parse(reorderFlags(rest))
		name := ""
		if fs.NArg() > 0 {
			name = fs.Arg(0)
		}
		body := map[string]any{"name": name}
		if *note != "" {
			body["note"] = *note
		}
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
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		fmt.Println("key:", r["key"])
		fmt.Println("name:", r["name"])
		fmt.Println("NOTE: shown only once — store it now.")
		return nil
	case "show":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key show <id> [-json]")
		}
		kfs := flag.NewFlagSet("key show", flag.ExitOnError)
		kjson := kfs.Bool("json", false, "print the raw key JSON (for scripts)")
		kfs.Parse(reorderFlags(rest[1:]))
		code, out, err := cc.Get("/api/v1/keys/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key show returned %d: %s", code, out)
		}
		if *kjson {
			fmt.Print(string(out))
			return nil
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
			LifetimeReqs       int64      `json:"lifetimeRequests"`
			LifetimeErrors     int64      `json:"lifetimeErrors"`
			LifetimePrompt     int64      `json:"lifetimePromptTokens"`
			LifetimeComp       int64      `json:"lifetimeCompletionTokens"`
			LifetimeCost       float64    `json:"lifetimeCost"`
			Note               string     `json:"note"`
			Disabled           bool       `json:"disabled"`
		}
		if err := json.Unmarshal(out, &kv); err != nil {
			return err
		}
		fmt.Printf("id:       %s\nname:     %s\nprefix:   %s…\ncreated:  %s\n", kv.ID, kv.Name, kv.Prefix, kv.CreatedAt.Local().Format("2006-01-02 15:04"))
		if kv.Disabled {
			fmt.Printf("status:   disabled\n")
		}
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
		fmt.Printf("lifetime: %d reqs · %d err · %d in · %d out · $%.4f\n",
			kv.LifetimeReqs, kv.LifetimeErrors, kv.LifetimePrompt, kv.LifetimeComp, kv.LifetimeCost)
		if kv.Note != "" {
			fmt.Printf("note:     %s\n", kv.Note)
		}
		return nil
	case "update":
		ufs := flag.NewFlagSet("key update", flag.ExitOnError)
		name := ufs.String("name", "", "rename the key")
		note := ufs.String("note", "", "free-text annotation")
		models := ufs.String("models", "", "comma-separated allowed model ids (empty string = all)")
		quota := ufs.Int64("quota", -1, "token quota (0 = unlimited)")
		maxReq := ufs.Int64("maxreq", -1, "total request cap (0 = unlimited)")
		rps := ufs.Float64("rps", -1, "per-key rate limit, req/s (0 = gateway default)")
		burst := ufs.Int("burst", -1, "rate-limit burst (0 = gateway default)")
		expires := ufs.Int("expires", -1, "expire after N days (0 = never)")
		budget := ufs.Float64("budget", -1, "daily USD budget (0 = unlimited)")
		maxConc := ufs.Int("max-concurrent", -1, "concurrent request cap (0 = unlimited)")
		ufs.Parse(reorderFlags(rest))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key update <id> [-name X] [-note X] [-models a,b] [-quota N] [-maxreq N] [-rps X] [-burst N] [-expires D] [-budget X] [-max-concurrent N]")
		}
		body := map[string]any{}
		ufs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "name":
				body["name"] = *name
			case "note":
				body["note"] = *note
			case "models":
				list := []string{}
				for _, m := range strings.Split(*models, ",") {
					if m = strings.TrimSpace(m); m != "" {
						list = append(list, m)
					}
				}
				body["allowedModels"] = list
			case "quota":
				body["tokenQuota"] = *quota
			case "maxreq":
				body["maxRequests"] = *maxReq
			case "rps":
				body["rateRPS"] = *rps
			case "burst":
				body["rateBurst"] = *burst
			case "expires":
				body["expiresInDays"] = *expires
			case "budget":
				body["dailyCostBudgetUSD"] = *budget
			case "max-concurrent":
				body["maxConcurrent"] = *maxConc
			}
		})
		if len(body) == 0 {
			return fmt.Errorf("nothing to update (pass at least one flag)")
		}
		code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(ufs.Arg(0)), body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key update returned %d: %s", code, out)
		}
		fmt.Printf("updated key %s\n", ufs.Arg(0))
		return nil
	case "note":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key note <id> <text...>  (empty text clears)")
		}
		note := strings.TrimSpace(strings.Join(rest[1:], " "))
		code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(rest[0]), map[string]any{"note": note})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key note returned %d: %s", code, out)
		}
		if note == "" {
			fmt.Println("note cleared")
		} else {
			fmt.Printf("note set: %s\n", note)
		}
		return nil
	case "disable", "enable":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key %s <id>", sub)
		}
		disabled := sub == "disable"
		code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(rest[0]), map[string]any{"disabled": disabled})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key %s returned %d: %s", sub, code, out)
		}
		fmt.Printf("key %s %sd\n", rest[0], sub)
		return nil
	case "list":
		fs := flag.NewFlagSet("key list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw keys JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/keys")
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
			Note               string     `json:"note"`
			Disabled           bool       `json:"disabled"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no keys")
			return nil
		}
		fmt.Printf("%-17s %-14s %-10s %10s %9s %8s %-12s %-12s %-9s %s\n", "ID", "NAME", "PREFIX", "TOKENS", "COST $", "REQS", "EXPIRES", "LAST USED", "QUOTA", "NOTE")
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
			if k.Disabled {
				quota = "disabled"
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
			note := k.Note
			if len(note) > 28 {
				note = note[:25] + "..."
			}
			fmt.Printf("%-17s %-14s %-10s %10d %9.4f %8d %-12s %-12s %-9s %s\n",
				k.ID, k.Name, k.Prefix+"…", k.UsedTok, k.UsedCost, k.UsedReqs, expires, last, quota, note)
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
		pfs := flag.NewFlagSet("key prune", flag.ExitOnError)
		dry := pfs.Bool("dry-run", false, "list expired keys without deleting them")
		pfs.Parse(reorderFlags(rest))
		prunePath := "/api/v1/keys/prune"
		if *dry {
			prunePath += "?dry=1"
		}
		code, out, err := cc.Post(prunePath, nil)
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
		verb := "removed"
		if *dry {
			verb = "would remove"
		}
		fmt.Printf("%s %d expired key(s)\n", verb, len(r.Removed))
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
	case "rotate":
		rfs := flag.NewFlagSet("key rotate", flag.ExitOnError)
		rjson := rfs.Bool("json", false, "print the raw rotation JSON, including the new secret (for scripts)")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key rotate <id> [-json]")
		}
		code, out, err := cc.Post("/api/v1/keys/"+rfs.Arg(0)+"/rotate", map[string]any{})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("rotate returned %d: %s", code, out)
		}
		if *rjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Println(string(out))
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
		fs := flag.NewFlagSet("autostart list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw autostart JSON (for scripts)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/autostart")
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
	system := fs.String("system", "", "system prompt prepended to the conversation (F188)")
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
	if strings.TrimSpace(*system) != "" {
		history = append(history, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
	}
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
	// `llmctl backup verify <file>` checks any local archive offline (F262),
	// complementing `llmctl backups verify <name>` for stored snapshots.
	if len(args) > 0 && args[0] == "verify" {
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl backup verify <file.tar.gz>")
		}
		entries, err := backup.Verify(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("backup %s ok — %d entries, state.json valid\n", args[1], entries)
		return nil
	}
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
