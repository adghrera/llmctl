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
	"sync"
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
	case "gpu":
		err = cmdGPU(args)
	case "disk":
		err = cmdDisk(args)
	case "presets":
		err = cmdPresets(args)
	case "catalog":
		err = cmdCatalog(args)
	case "maxparallel":
		err = cmdMaxParallel(args)
	case "jobs":
		err = cmdJobs(args)
	case "search":
		err = cmdWebSearch(args)
	case "structured":
		err = cmdStructured(args)
	case "embed":
		err = cmdEmbed(args)
	case "complete":
		err = cmdComplete(args)
	case "compare":
		err = cmdCompare(args)
	case "count":
		err = cmdCount(args)
	case "bench":
		err = cmdBench(args)
	case "note":
		err = cmdNote(args)
	case "autorestart":
		err = cmdAutoRestartToggle(args)
	case "setvars":
		err = cmdSetVars(args)
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
	case "completion":
		err = cmdCompletion(args)
	case "version", "--version", "-v":
		if len(args) > 0 && (args[0] == "--json" || args[0] == "-json") { // F440
			daemonVer := ""
			if cc, cerr := daemon.NewControlClient(); cerr == nil {
				if code, out, cerr := cc.Get("/api/v1/status"); cerr == nil && code == 200 {
					var s struct {
						Version string `json:"version"`
					}
					if json.Unmarshal(out, &s) == nil {
						daemonVer = s.Version
					}
				}
			}
			b, _ := json.Marshal(map[string]string{"cli": version, "daemon": daemonVer})
			fmt.Println(string(b))
			break
		}
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
  llmctl status [-json] [-watch N] [-out FILE]
  llmctl doctor [-json] [--csv] [-quiet] [-check NAME]... [-out FILE]
  llmctl gpu [-json] [--csv] [-out FILE]
  llmctl jobs [-json] [--csv] [-watch N] [-out FILE]
  llmctl search <query> [-max N] [-json] [--csv] [-out FILE]
  llmctl structured -model ID -schema file.json [-max-tokens N] [-temperature T] [-out FILE] <prompt>
  llmctl embed -model ID [-json] [-out FILE] <text>
  llmctl complete -model ID [-json] [-out FILE] <prompt>
  llmctl compare -models A,B [-system S] [-max-tokens N] [-temperature T] [-json] [-out FILE] <prompt>
  llmctl count <text> [-file F] [-chat ID] [-model ID] [-json] [-out FILE]   (estimate tokens, ~4 chars/token)
  llmctl bench -model ID [-n N] [-conc C]
  llmctl bench -models A,B [-n N] [-conc C] [-json|--csv] [-out FILE]   (benchmark + comparison table)
  llmctl disk [-json] [--csv] [-out FILE]
  llmctl ps [-watch N] [--csv] [-json] [-out FILE]
  llmctl backends list [-json|-csv]
  llmctl backends install <id> [--asset <hint>]
  llmctl backends uninstall <id>
  llmctl backends add <plugin.json>
  llmctl backends remove <id>
  llmctl models list [--csv] [-out FILE]
  llmctl models show <id> [-json] [-out FILE]
  llmctl models probe
  llmctl models ctx <id> <tokens> | price <id> <in/M> <out/M> | idle <id> <min|never>
  llmctl models metadata <id> [-json]
  llmctl models unused [--csv]
  llmctl models routable [-key K] [-json|-csv]   (what the gateway advertises on /v1/models)
  llmctl models add <id> <file> [-backend ID]
  llmctl models aliases [--csv]
  llmctl models alias <name> <model-id>
  llmctl models unalias <name>
  llmctl models install <repo-or-url> [--file f.gguf] [--rev main] [--backend llama.cpp] [--id ID]
  llmctl models remove <id> [--file]
  llmctl models ctx <model-id> <tokens|0>
  llmctl models price <model-id> <in-per-M> <out-per-M>
  llmctl models hf-search <query>
  llmctl models hf-files <repo>
  llmctl start <model> [--backend ID] [--instance NAME] [--var k=v ...]
  llmctl stop <instance> [--force] | llmctl stop --all
  llmctl restart <instance> | llmctl restart --all
  llmctl maxparallel <instance> <n>   (0 = unlimited)
  llmctl note <instance> <text> | autorestart <instance> <on|off> | setvars <instance> [-var k=v…]
  llmctl logs <instance> [--stream stderr|stdout] [--tail N] [-out FILE] [-f]
  llmctl logs -daemon [--tail N] [-f]
  llmctl key generate [name] [-json]
  llmctl key show <id> [-json]
  llmctl key note <id> <text...>
  llmctl key update <id> [-name X] [-quota N] [-models a,b] …
  llmctl key disable <id> / llmctl key enable <id>
  llmctl key rotate <id> [-json]
  llmctl key list [--csv] [-out FILE]
  llmctl key revoke <id>
  llmctl key reset-usage <id>
  llmctl autostart list [--csv]
  llmctl autostart add <model> [--backend ID] [--instance NAME] [--port N] [--var k=v ...]
  llmctl autostart remove <model>
  llmctl autostart export [-out FILE] | autostart import <file.json>
  llmctl presets list [-json|-csv]|save -name N -model ID [...]|start <name>|delete <name>|export|import
  llmctl catalog list [-json|-csv] | install <id>
  llmctl scheduler list
  llmctl scheduler show <id> [-json]
  llmctl scheduler add --name N --cron "0 9 * * *" [--kind chat|instance.start|instance.stop|backup] [--model ID] [--prompt TEXT] [--instance ID] [--kb ID]
  llmctl scheduler run|enable|disable|delete <id>
  llmctl scheduler update <id> [-name N] [-cron C] [-model M] [-prompt P] [-enabled true|false] [-kb ID]
  llmctl scheduler runs [-n 20] [--csv] [-out FILE]
  llmctl scheduler export [-out FILE] | scheduler import <file.json>
  llmctl requests [--model ID] [--status 4|404] [--min-ms N] [--key PREFIX] [--instance ID] [--id ID] [-n 20] [-json] [-watch N] [-stats [-by model|key|instance]] [--csv on list and stats] [-out FILE]
  llmctl backups [list|create [-name LABEL]|restore <name>|verify <name>|download <name>|delete <name>] [-json|-csv|-out FILE on list]
  llmctl usage [-n 14] [-key ID] [-keys] [-model ID] [--csv] [--json] [-out FILE]
  llmctl metrics [-watch N] [-model M] [--csv] [-json] [-out FILE]
  llmctl events [-n 20] [-kind K] [-model M] [-csv] [-json] [-out FILE]
  llmctl webhooks [-n 20] [-event E] [--csv] [-json] [-out FILE] | webhooks test   recent webhook delivery attempts / send a test event
  llmctl prompts [list] [filter] [-category X] [-json] [-csv] | llmctl prompts show <id> [-out file] | prompts render <id> [-input TEXT] [-var k=v…] [-out file] | prompts categories | prompts add <id> -name N [-content T] | prompts update <id> [-name N] | prompts delete <id> | prompts export|import
  llmctl chats [filter] [-n 20] [--csv] [-json] | chats list [filter] | chats show|search|export|export-all|import|fork|rename|pin|unpin|prune|delete|delete-all …
  llmctl skills [filter] [-json] [-csv] | skills show|add|export|enable|disable|delete <id>
  llmctl memory [filter] [-n 30] [-json|-csv] | memory show|add|update|search|export|import|delete …
  llmctl kb [filter] [--csv] [-json] | kb show|create|update|docs|doc|add-doc|export|search|embed|delete …
  llmctl tools [-json|-csv|-out FILE] | tools show|install|enable|disable|run|delete <id>
  llmctl mcp [-json|-csv|-out FILE] | mcp add|remove|enable|disable|test <id …> | mcp export|import
  llmctl cost [-keys] [-model M] [-key K] [--csv] [-json] [-out FILE]
  llmctl files [list|upload <path>|cat <id>|delete <id>] [--csv] [-json] [-out FILE on list]
  llmctl settings [-json] [-out FILE] | llmctl settings set <flags> | settings test-webhook
  llmctl batch -model ID [-system S] [-key K] [-concurrency N] [-temperature T] [-file F|piped stdin|prompts…] [-json] [-out FILE]
  llmctl chat [--model ID] [--key KEY] [-prompt TEXT | piped stdin = one-shot] [-agent] [-save TITLE | -chat ID | -continue] [-kb ID] [-json] [-temperature T] [-max-tokens N] [-out FILE]
                                     interactive REPL supports /help /clear /system TEXT /model ID /save [title] /quit (F331)
  llmctl backup [file.tar.gz]     export state (config/chats/KBs/memory/…)
  llmctl backup verify <file>     check any local archive offline
  llmctl restore <file.tar.gz>    import a backup into the state dir
  llmctl version [--json]
  llmctl completion [bash|zsh|fish|powershell]  print a shell completion script

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
	outFile := fs.String("out", "", "write the raw status JSON to this file instead of stdout (F466)")
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
		if *outFile != "" { // F466: latest snapshot to a file (each -watch refresh overwrites)
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			if *watch <= 0 {
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			}
			return nil
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
	csvOut := fs.Bool("csv", false, "output the checks as CSV (F429)")
	outFile := fs.String("out", "", "write the doctor JSON to this file instead of stdout (F464)")
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
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F464)
		if len(only) > 0 {
			b, err := json.Marshal(res)
			if err != nil {
				return err
			}
			if *outFile != "" {
				if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
			} else {
				fmt.Println(string(b))
			}
		} else {
			if *outFile != "" {
				if err := os.WriteFile(*outFile, out, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			} else {
				fmt.Print(string(out))
			}
		}
		if !res.OK {
			return fmt.Errorf("doctor: some checks failed")
		}
		return nil
	}
	sym := map[string]string{"ok": "[ok]  ", "warn": "[warn]", "error": "[fail]"}
	if *csvOut { // F429: header-only when -quiet leaves nothing
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"name", "status", "detail"})
		for _, c := range res.Checks {
			if *quiet && c.Status == "ok" {
				continue
			}
			_ = w.Write([]string{c.Name, c.Status, c.Detail})
		}
		w.Flush()
		if !res.OK {
			return fmt.Errorf("doctor: some checks failed")
		}
		return w.Error()
	}
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

// cmdGPU prints GPU inventory from the daemon's probe (GET /api/v1/gpu).
func cmdGPU(args []string) error {
	fs := flag.NewFlagSet("gpu", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw GPU JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the GPU list as CSV (F430)")
	outFile := fs.String("out", "", "write the raw GPU JSON to this file instead of stdout (F463)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/gpu")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("gpu returned %d: %s", code, out)
	}
	if *outFile != "" { // F463: raw JSON to a file, stdout stays clean
		if err := os.WriteFile(*outFile, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		return nil
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	var res struct {
		GPUs []struct {
			Vendor     string `json:"vendor"`
			Name       string `json:"name"`
			TotalBytes int64  `json:"total_bytes"`
			UsedBytes  int64  `json:"used_bytes"`
		} `json:"gpus"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	if *csvOut { // F430: header-only when no GPUs detected
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"vendor", "name", "total_bytes", "used_bytes", "used_pct"})
		for _, g := range res.GPUs {
			pct := 0.0
			if g.TotalBytes > 0 {
				pct = 100 * float64(g.UsedBytes) / float64(g.TotalBytes)
			}
			_ = w.Write([]string{g.Vendor, g.Name,
				strconv.FormatInt(g.TotalBytes, 10), strconv.FormatInt(g.UsedBytes, 10),
				strconv.FormatFloat(pct, 'f', 1, 64)})
		}
		w.Flush()
		return w.Error()
	}
	if len(res.GPUs) == 0 {
		fmt.Println("no GPUs detected")
		return nil
	}
	fmt.Printf("%-8s %-32s %10s %10s %6s\n", "VENDOR", "NAME", "TOTAL", "USED", "%")
	for _, g := range res.GPUs {
		pct := 0.0
		if g.TotalBytes > 0 {
			pct = 100 * float64(g.UsedBytes) / float64(g.TotalBytes)
		}
		fmt.Printf("%-8s %-32s %10s %10s %5.1f%%\n", g.Vendor, g.Name, fmtSize(g.TotalBytes), fmtSize(g.UsedBytes), pct)
	}
	return nil
}

// cmdDisk prints model-disk usage from the daemon (GET /api/v1/disk).
func cmdDisk(args []string) error {
	fs := flag.NewFlagSet("disk", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw disk JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the disk report as CSV (F451)")
	outFile := fs.String("out", "", "write the raw disk JSON to this file instead of stdout (F465)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/disk")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("disk returned %d: %s", code, out)
	}
	if *outFile != "" { // F465: raw JSON to a file, stdout stays clean
		if err := os.WriteFile(*outFile, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		return nil
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	var res struct {
		Dir         string `json:"dir"`
		TotalBytes  int64  `json:"totalBytes"`
		FreeBytes   int64  `json:"freeBytes"`
		ModelsBytes int64  `json:"modelsBytes"`
		ModelsCount int    `json:"modelsCount"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	if *csvOut { // F451: raw byte values, one row
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"dir", "models_count", "models_bytes", "free_bytes", "total_bytes"})
		_ = w.Write([]string{res.Dir, strconv.Itoa(res.ModelsCount),
			strconv.FormatInt(res.ModelsBytes, 10),
			strconv.FormatInt(res.FreeBytes, 10),
			strconv.FormatInt(res.TotalBytes, 10)})
		w.Flush()
		return w.Error()
	}
	fmt.Printf("dir:     %s\n", res.Dir)
	fmt.Printf("models:  %d file(s), %s\n", res.ModelsCount, fmtSize(res.ModelsBytes))
	fmt.Printf("volume:  %s free of %s\n", fmtSize(res.FreeBytes), fmtSize(res.TotalBytes))
	return nil
}

// sortedVars renders a vars map as stable "k=v k=v" text for tables.
func sortedVars(m map[string]string) string {
	return sortedVarsJoin(m, " ")
}

func sortedVarsJoin(m map[string]string, sep string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, sep)
}

// cmdPresets manages start presets (named start recipes from the Instances
// page): list, save, start, delete (F265).
func cmdPresets(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl presets <list|save|start|delete|export|import> [flags]")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "export":
		// Dump all launch presets as a JSON array (feed to `presets import`, F328).
		efs := flag.NewFlagSet("presets export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/presets")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("presets export: %s", out)
		}
		var list []map[string]any
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %d presets to %s\n", len(list), *outFile)
			return nil
		}
		fmt.Print(string(data))
		return nil
	case "import":
		// Create/replace presets from a `presets export` file (F328);
		// same-name presets are replaced.
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl presets import <file.json>")
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		var list []map[string]any
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		var created, skipped int
		for _, p := range list {
			code, out, err := cc.Post("/api/v1/presets", p)
			if err != nil {
				return err
			}
			if code/100 != 2 {
				fmt.Printf("  skip %v: %s\n", p["name"], out)
				skipped++
				continue
			}
			created++
		}
		fmt.Printf("imported %d presets (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("presets list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw presets JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the preset list as CSV (F386)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/presets")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("presets returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			Name      string            `json:"name"`
			Instance  string            `json:"instance"`
			ModelID   string            `json:"modelId"`
			BackendID string            `json:"backendId"`
			Port      int               `json:"port"`
			Vars      map[string]string `json:"vars"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if *csvOut { // F386: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"name", "model", "instance", "backend", "port", "vars"})
			for _, p := range list {
				_ = w.Write([]string{p.Name, p.ModelID, p.Instance, p.BackendID,
					strconv.Itoa(p.Port), sortedVarsJoin(p.Vars, ";")})
			}
			w.Flush()
			return w.Error()
		}
		if len(list) == 0 {
			fmt.Println("no presets (save one: llmctl presets save -name N -model ID)")
			return nil
		}
		fmt.Printf("%-18s %-16s %-12s %-7s %s\n", "NAME", "MODEL", "INSTANCE", "PORT", "VARS")
		for _, p := range list {
			inst := p.Instance
			if inst == "" {
				inst = p.ModelID
			}
			port := "auto"
			if p.Port > 0 {
				port = strconv.Itoa(p.Port)
			}
			fmt.Printf("%-18s %-16s %-12s %-7s %s\n", p.Name, p.ModelID, inst, port, sortedVars(p.Vars))
		}
		return nil
	case "save":
		fs := flag.NewFlagSet("presets save", flag.ExitOnError)
		name := fs.String("name", "", "preset name (required)")
		model := fs.String("model", "", "model id (required)")
		instance := fs.String("instance", "", "instance name (default: model id)")
		backendID := fs.String("backend", "", "backend id (default: model's)")
		port := fs.Int("port", 0, "fixed port (0 = auto-assign)")
		var vars stringList
		fs.Var(&vars, "var", "parameter k=v (repeatable)")
		fs.Parse(reorderFlags(rest))
		if *name == "" || *model == "" {
			return fmt.Errorf("usage: llmctl presets save -name N -model ID [-instance I] [-backend B] [-port P] [-var k=v ...]")
		}
		body := map[string]any{"name": *name, "modelId": *model}
		if *instance != "" {
			body["instance"] = *instance
		}
		if *backendID != "" {
			body["backendId"] = *backendID
		}
		if *port > 0 {
			body["port"] = *port
		}
		if m := vars.Map(); len(m) > 0 {
			body["vars"] = m
		}
		code, out, err := cc.Post("/api/v1/presets", body)
		if err != nil {
			return err
		}
		if code != 200 && code != 201 {
			return fmt.Errorf("presets save returned %d: %s", code, out)
		}
		fmt.Printf("preset %s saved\n", *name)
		return nil
	case "start":
		fs := flag.NewFlagSet("presets start", flag.ExitOnError)
		port := fs.Int("port", 0, "override the preset port")
		var vars stringList
		fs.Var(&vars, "var", "parameter k=v override (repeatable)")
		fs.Parse(reorderFlags(rest))
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl presets start <name> [-port P] [-var k=v ...]")
		}
		name := fs.Arg(0)
		body := map[string]any{}
		if *port > 0 {
			body["port"] = *port
		}
		if m := vars.Map(); len(m) > 0 {
			body["vars"] = m
		}
		var b any
		if len(body) > 0 {
			b = body
		}
		code, out, err := cc.Post("/api/v1/presets/"+url.PathEscape(name)+"/start", b)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("preset start failed: %s", out)
		}
		var inst struct {
			ID    string `json:"id"`
			Port  int    `json:"port"`
			Model string `json:"modelId"`
		}
		json.Unmarshal(out, &inst)
		fmt.Printf("preset %s started — instance %s on port %d\n", name, inst.ID, inst.Port)
		return nil
	case "delete":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl presets delete <name>")
		}
		code, out, err := cc.Delete("/api/v1/presets/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("presets delete returned %d: %s", code, out)
		}
		fmt.Printf("preset %s deleted\n", rest[0])
		return nil
	default:
		return fmt.Errorf("unknown presets subcommand: %s (list|save|start|delete)", sub)
	}
}

// cmdCatalog browses the curated model catalog (GET /api/v1/catalog) and
// installs entries by id (F266).
func cmdCatalog(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl catalog <list|install> [id] [flags]")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("catalog list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw catalog JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the catalog as CSV (F387)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/catalog")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("catalog returned %d: %s", code, out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Backend string   `json:"backend"`
			SizeGB  string   `json:"sizeGB"`
			Quant   string   `json:"quant"`
			Tags    []string `json:"tags"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if *csvOut { // F387: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "backend", "size_gb", "quant", "tags"})
			for _, e := range list {
				_ = w.Write([]string{e.ID, e.Name, e.Backend, e.SizeGB, e.Quant, strings.Join(e.Tags, ";")})
			}
			w.Flush()
			return w.Error()
		}
		fmt.Printf("%-28s %-28s %-10s %-8s %-10s %s\n", "ID", "NAME", "SIZE", "QUANT", "BACKEND", "TAGS")
		for _, e := range list {
			fmt.Printf("%-28s %-28s %-10s %-8s %-10s %s\n", e.ID, e.Name, e.SizeGB+" GB", e.Quant, e.Backend, strings.Join(e.Tags, ","))
		}
		fmt.Printf("\ninstall one: llmctl catalog install <id>\n")
		return nil
	case "install":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl catalog install <id>  (see: llmctl catalog list)")
		}
		code, out, err := cc.Post("/api/v1/catalog/install", map[string]any{"id": rest[0]})
		if err != nil {
			return err
		}
		if code != 200 && code != 202 {
			return fmt.Errorf("catalog install returned %d: %s", code, out)
		}
		fmt.Printf("catalog install %s started — track with: llmctl models show %s\n", rest[0], rest[0])
		return nil
	default:
		return fmt.Errorf("unknown catalog subcommand: %s (list|install)", sub)
	}
}

// cmdMaxParallel caps how many simultaneous gateway requests an instance
// serves (PATCH /api/v1/instances/{id}/maxparallel, F270).
func cmdMaxParallel(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: llmctl maxparallel <instance-id> <n>  (0 = unlimited)")
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 0 {
		return fmt.Errorf("invalid maxParallel: %s (must be >= 0)", args[1])
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Do("PATCH", "/api/v1/instances/"+url.PathEscape(args[0])+"/maxparallel",
		map[string]any{"maxParallel": n})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("maxparallel: %s", out)
	}
	label := "unlimited"
	if n > 0 {
		label = strconv.Itoa(n)
	}
	fmt.Printf("instance %s now serves at most %s concurrent request(s)\n", args[0], label)
	return nil
}

// cmdNote sets the free-text note on an instance (PATCH note endpoint, F119).
func cmdNote(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: llmctl note <instance-id> <text…>")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	note := strings.Join(args[1:], " ")
	code, out, err := cc.Do("PATCH", "/api/v1/instances/"+url.PathEscape(args[0])+"/note",
		map[string]any{"note": note})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("note: %s", out)
	}
	fmt.Printf("instance %s note set\n", args[0])
	return nil
}

// cmdAutoRestartToggle toggles crash auto-restart on an instance (F087).
func cmdAutoRestartToggle(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: llmctl autorestart <instance-id> <on|off>")
	}
	on := strings.ToLower(args[1]) == "on"
	if s := strings.ToLower(args[1]); s != "on" && s != "off" {
		return fmt.Errorf("expected on or off, got %q", args[1])
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Post("/api/v1/instances/"+url.PathEscape(args[0])+"/autorestart",
		map[string]any{"enabled": on})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("autorestart: %s", out)
	}
	state := "off"
	if on {
		state = "on"
	}
	fmt.Printf("instance %s auto-restart %s\n", args[0], state)
	return nil
}

// cmdSetVars replaces the stored start vars of an instance (PUT vars, F091).
func cmdSetVars(args []string) error {
	fs := flag.NewFlagSet("setvars", flag.ExitOnError)
	var vars stringList
	fs.Var(&vars, "var", "parameter k=v (repeatable)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl setvars <instance-id> [-var k=v ...]")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Do("PUT", "/api/v1/instances/"+url.PathEscape(fs.Arg(0))+"/vars",
		map[string]any{"vars": vars.Map()})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("setvars: %s", out)
	}
	fmt.Printf("instance %s vars updated (restart to apply)\n", fs.Arg(0))
	return nil
}

// cmdJobs lists background jobs (model/backend installs) tracked by the
// daemon (GET /api/v1/jobs, F273).
func cmdJobs(args []string) error {
	fs := flag.NewFlagSet("jobs", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw jobs JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the job list as CSV (F427)")
	watch := fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once)")
	outFile := fs.String("out", "", "write the raw jobs JSON to this file instead of stdout (F477)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	printOnce := func() error {
		code, out, err := cc.Get("/api/v1/jobs")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("jobs returned %d: %s", code, out)
		}
		if *outFile != "" { // F477: latest snapshot to a file (each -watch refresh overwrites)
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			if *watch <= 0 {
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			}
			return nil
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID       string    `json:"id"`
			Kind     string    `json:"kind"`
			Target   string    `json:"target"`
			Status   string    `json:"status"`
			Progress float64   `json:"progress"`
			Message  string    `json:"message"`
			Error    string    `json:"error"`
			Updated  time.Time `json:"updated"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if *csvOut { // F427: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "kind", "target", "status", "progress", "message", "error", "updated"})
			for _, j := range list {
				_ = w.Write([]string{j.ID, j.Kind, j.Target, j.Status,
					strconv.FormatFloat(j.Progress, 'f', 2, 64), j.Message, j.Error,
					j.Updated.UTC().Format(time.RFC3339)})
			}
			w.Flush()
			return w.Error()
		}
		if len(list) == 0 {
			fmt.Println("no jobs (installs appear here: llmctl models install …)")
			return nil
		}
		fmt.Printf("%-16s %-20s %-9s %5s %s\n", "KIND", "TARGET", "STATUS", "%", "MESSAGE")
		for _, j := range list {
			msg := j.Message
			if j.Error != "" {
				msg = "error: " + j.Error
			}
			if len(msg) > 50 {
				msg = msg[:50] + "…"
			}
			fmt.Printf("%-16s %-20s %-9s %4.0f%% %s\n", j.Kind, j.Target, j.Status, j.Progress*100, msg)
		}
		return nil
	}
	if *watch <= 0 {
		return printOnce()
	}
	for {
		if err := printOnce(); err != nil {
			return err
		}
		time.Sleep(time.Duration(*watch) * time.Second)
	}
}

// cmdWebSearch runs the daemon's keyless web search (GET /api/v1/search, F274).
func cmdWebSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	max := fs.Int("max", 5, "max results")
	jsonOut := fs.Bool("json", false, "print the raw search JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the results as CSV (F428)")
	outFile := fs.String("out", "", "write the raw search JSON to this file instead of stdout (F462)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl search <query> [-max N] [-json]")
	}
	q := strings.Join(fs.Args(), " ")
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/search?q=" + url.QueryEscape(q) + "&max=" + strconv.Itoa(*max))
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("search: %s", out)
	}
	if *outFile != "" { // F462: raw JSON to a file, stdout stays clean
		if err := os.WriteFile(*outFile, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		return nil
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	var res struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	if *csvOut { // F428: header-only when empty keeps the stream parseable
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"rank", "title", "url", "snippet"})
		for i, r := range res.Results {
			_ = w.Write([]string{strconv.Itoa(i + 1), r.Title, r.URL,
				strings.TrimSpace(strings.ReplaceAll(r.Snippet, "\n", " "))})
		}
		w.Flush()
		return w.Error()
	}
	if len(res.Results) == 0 {
		fmt.Printf("no results for %q\n", q)
		return nil
	}
	for i, r := range res.Results {
		sn := strings.TrimSpace(strings.ReplaceAll(r.Snippet, "\n", " "))
		if len(sn) > 120 {
			sn = sn[:120] + "…"
		}
		fmt.Printf("%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if sn != "" {
			fmt.Printf("   %s\n", sn)
		}
	}
	return nil
}

// cmdStructured runs the daemon's schema-validated JSON generation
// (POST /api/v1/structured, F279).
func cmdStructured(args []string) error {
	fs := flag.NewFlagSet("structured", flag.ExitOnError)
	model := fs.String("model", "", "model id (required)")
	schemaF := fs.String("schema", "", "path to a JSON Schema file (required)")
	keyF := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled; default $LLMCTL_API_KEY)")
	retries := fs.Int("retries", 2, "validation retries")
	maxTok := fs.Int("max-tokens", 0, "max tokens per attempt (0 = model default, F344)")
	temp := fs.Float64("temperature", -1, "sampling temperature (-1 = model default, F344)")
	outFile := fs.String("out", "", "write the validated JSON to this file instead of stdout (F457)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 || *model == "" || *schemaF == "" {
		return fmt.Errorf("usage: llmctl structured -model ID -schema file.json [-key K] [-retries N] [-max-tokens N] [-temperature T] <prompt>")
	}
	schemaBytes, err := os.ReadFile(*schemaF)
	if err != nil {
		return err
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return fmt.Errorf("schema file: %w", err)
	}
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			prompt = strings.TrimSpace(string(b))
		}
	}
	if prompt == "" {
		return fmt.Errorf("no prompt (pass it as an argument or pipe it to stdin)")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	body := map[string]any{
		"model":    *model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"schema":   schema,
	}
	if *retries > 0 {
		body["maxRetries"] = *retries
	}
	if *maxTok > 0 {
		body["maxTokens"] = *maxTok
	}
	if *temp >= 0 {
		body["temperature"] = *temp
	}
	code, out, err := cc.DoKey("POST", "/api/v1/structured", body, *keyF)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("structured: %s", out)
	}
	var res struct {
		OK       bool            `json:"ok"`
		JSON     json.RawMessage `json:"json"`
		Attempts int             `json:"attempts"`
		Error    string          `json:"error"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("structured failed after retries: %s", res.Error)
	}
	if *outFile != "" { // F457: validated JSON only, for scripts
		if err := os.WriteFile(*outFile, append(res.JSON, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(res.JSON)+1)
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, res.JSON, "", "  "); err != nil {
		fmt.Println(string(res.JSON))
		return nil
	}
	fmt.Println(pretty.String())
	return nil
}

// cmdEmbed calls the gateway's /v1/embeddings endpoint (F281).
func cmdEmbed(args []string) error {
	fs := flag.NewFlagSet("embed", flag.ExitOnError)
	model := fs.String("model", "", "embedding model id (required)")
	keyF := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled; default $LLMCTL_API_KEY)")
	jsonOut := fs.Bool("json", false, "print the raw embeddings JSON (for scripts)")
	outFile := fs.String("out", "", "write the raw embeddings JSON to this file instead of stdout (implies -json, F456)")
	fs.Parse(reorderFlags(args))
	if *model == "" {
		return fmt.Errorf("usage: llmctl embed -model ID [-key K] [-json] <text…>  (or pipe text to stdin)")
	}
	text := strings.Join(fs.Args(), " ")
	if text == "" {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			text = strings.TrimSpace(string(b))
		}
	}
	if text == "" {
		return fmt.Errorf("no text (pass it as an argument or pipe it to stdin)")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.DoKey("POST", "/v1/embeddings", map[string]any{
		"model": *model, "input": text,
	}, *keyF)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("embed: %s", out)
	}
	if *outFile != "" { // F456: raw JSON to a file, stdout stays clean
		if err := os.WriteFile(*outFile, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		return nil
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	var res struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &res); err != nil || len(res.Data) == 0 {
		return fmt.Errorf("embed: unexpected response: %s", out)
	}
	v := res.Data[0].Embedding
	fmt.Printf("model %s: %d dimensions\n", *model, len(v))
	for i := 0; i < len(v) && i < 6; i++ {
		fmt.Printf("  [%d] %.6f\n", i, v[i])
	}
	if len(v) > 6 {
		fmt.Printf("  … (%d more)\n", len(v)-6)
	}
	return nil
}

// cmdBench measures gateway chat throughput/latency for one model (F282).
func cmdBench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	model := fs.String("model", "", "model id to benchmark (required, unless -models is used)")
	modelsFlag := fs.String("models", "", "comma-separated model ids to benchmark and compare (F303)")
	keyF := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled; default $LLMCTL_API_KEY)")
	n := fs.Int("n", 5, "number of requests")
	conc := fs.Int("conc", 1, "concurrent requests")
	prompt := fs.String("prompt", "count to five", "prompt for every request")
	jsonOut := fs.Bool("json", false, "print per-model stats as one JSON array instead of text (F315)")
	csvOut := fs.Bool("csv", false, "output per-model stats as CSV (F426)")
	outFile := fs.String("out", "", "write the stats JSON to this file instead of stdout (implies -json, F460)")
	fs.Parse(reorderFlags(args))
	if *outFile != "" {
		*jsonOut = true // -out implies the JSON shape (F460)
	}
	var models []string
	if *modelsFlag != "" {
		for _, m := range strings.Split(*modelsFlag, ",") {
			if m = strings.TrimSpace(m); m != "" {
				models = append(models, m)
			}
		}
	} else if *model != "" {
		models = []string{*model}
	}
	if len(models) == 0 {
		return fmt.Errorf("usage: llmctl bench -model ID [-key K] [-n N] [-conc C] [-prompt TEXT]\n       llmctl bench -models A,B [-key K] [-n N] [-conc C] [-prompt TEXT]")
	}
	if *n < 1 {
		*n = 1
	}
	if *conc < 1 {
		*conc = 1
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	type benchStats struct {
		model    string
		ok       int
		fails    int
		wall     time.Duration
		min      float64
		avg      float64
		p50      float64
		p95      float64
		max      float64
		reqps    float64
		tokps    float64
		firstErr string
	}
	benchOne := func(m string) (*benchStats, error) {
		body, _ := json.Marshal(map[string]any{
			"model":    m,
			"messages": []map[string]string{{"role": "user", "content": *prompt}},
		})
		type result struct {
			ms   float64
			tok  int
			errS string
		}
		// worker pool over a job channel
		jobs := make(chan int)
		res := make(chan result, *n)
		var wg sync.WaitGroup
		for w := 0; w < *conc; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range jobs {
					req, err := http.NewRequest(http.MethodPost, cc.Base+"/v1/chat/completions", strings.NewReader(string(body)))
					if err != nil {
						res <- result{errS: err.Error()}
						continue
					}
					req.Header.Set("Content-Type", "application/json")
					if *keyF != "" {
						req.Header.Set("Authorization", "Bearer "+*keyF)
					}
					hc := &http.Client{Timeout: 5 * time.Minute}
					t0 := time.Now()
					resp, err := hc.Do(req)
					if err != nil {
						res <- result{errS: err.Error()}
						continue
					}
					payload, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					ms := float64(time.Since(t0).Microseconds()) / 1000.0
					if resp.StatusCode != 200 {
						sn := strings.TrimSpace(string(payload))
						if len(sn) > 80 {
							sn = sn[:80] + "\u2026"
						}
						res <- result{errS: fmt.Sprintf("%d: %s", resp.StatusCode, sn)}
						continue
					}
					var parsed struct {
						Usage struct {
							TotalTokens int `json:"total_tokens"`
						} `json:"usage"`
					}
					json.Unmarshal(payload, &parsed)
					res <- result{ms: ms, tok: parsed.Usage.TotalTokens}
				}
			}()
		}
		go func() {
			for i := 0; i < *n; i++ {
				jobs <- i
			}
			close(jobs)
		}()
		go func() {
			wg.Wait()
			close(res)
		}()

		if !*jsonOut && !*csvOut {
			fmt.Printf("bench: model=%s n=%d conc=%d\n", m, *n, *conc)
		}
		var lats []float64
		var fails int
		var firstErr string
		var totalTok, done int
		t0 := time.Now()
		for r := range res {
			done++
			if r.errS != "" {
				fails++
				if firstErr == "" {
					firstErr = r.errS
				}
				continue
			}
			lats = append(lats, r.ms)
			totalTok += r.tok
			if !*jsonOut && !*csvOut && (done%10 == 0 || done == *n) {
				fmt.Printf("  %d/%d done\r", done, *n)
			}
		}
		elapsed := time.Since(t0)
		if !*jsonOut && !*csvOut {
			fmt.Println()
		}
		if fails > 0 && !*jsonOut && !*csvOut {
			fmt.Printf("errors: %d/%d (first: %s)\n", fails, *n, firstErr)
		}
		if len(lats) == 0 {
			return nil, fmt.Errorf("bench %s: no successful requests", m)
		}
		sort.Float64s(lats)
		pct := func(p float64) float64 {
			i := int(p * float64(len(lats)))
			if i >= len(lats) {
				i = len(lats) - 1
			}
			return lats[i]
		}
		var sum float64
		for _, v := range lats {
			sum += v
		}
		if !*jsonOut && !*csvOut {
			fmt.Printf("ok: %d  wall: %s\n", len(lats), elapsed.Round(time.Millisecond))
			fmt.Printf("latency ms: min %.0f  avg %.0f  p50 %.0f  p95 %.0f  max %.0f\n",
				lats[0], sum/float64(len(lats)), pct(0.50), pct(0.95), lats[len(lats)-1])
			fmt.Printf("throughput: %.2f req/s", float64(len(lats))/elapsed.Seconds())
			if totalTok > 0 {
				fmt.Printf("  %.0f tok/s", float64(totalTok)/elapsed.Seconds())
			}
			fmt.Println()
		}
		return &benchStats{
			model: m, ok: len(lats), fails: fails, wall: elapsed,
			min: lats[0], avg: sum / float64(len(lats)), p50: pct(0.50), p95: pct(0.95),
			max:      lats[len(lats)-1],
			reqps:    float64(len(lats)) / elapsed.Seconds(),
			tokps:    float64(totalTok) / elapsed.Seconds(),
			firstErr: firstErr,
		}, nil
	}
	var all []*benchStats
	var failed []map[string]any
	var firstErr error
	for _, m := range models {
		st, err := benchOne(m)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, map[string]any{"model": m, "error": err.Error()})
		}
		if st != nil {
			all = append(all, st)
		}
	}
	if *jsonOut {
		var arr []map[string]any
		for _, st := range all {
			arr = append(arr, map[string]any{
				"model": st.model, "ok": st.ok, "fails": st.fails,
				"wall_ms": st.wall.Milliseconds(),
				"min_ms":  st.min, "avg_ms": st.avg, "p50_ms": st.p50,
				"p95_ms": st.p95, "max_ms": st.max,
				"req_per_s": st.reqps, "tok_per_s": st.tokps,
				"first_error": st.firstErr,
			})
		}
		arr = append(arr, failed...)
		b, _ := json.Marshal(arr)
		if *outFile != "" { // F460: stats JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
			return nil
		}
		fmt.Println(string(b))
	} else if *csvOut { // F426: one row per model, header-only when none succeeded
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"model", "ok", "fails", "wall_ms", "min_ms", "avg_ms", "p50_ms", "p95_ms", "max_ms", "req_per_s", "tok_per_s", "first_error"})
		for _, st := range all {
			_ = w.Write([]string{st.model, strconv.Itoa(st.ok), strconv.Itoa(st.fails),
				strconv.FormatInt(st.wall.Milliseconds(), 10),
				strconv.FormatFloat(st.min, 'f', 0, 64), strconv.FormatFloat(st.avg, 'f', 0, 64),
				strconv.FormatFloat(st.p50, 'f', 0, 64), strconv.FormatFloat(st.p95, 'f', 0, 64),
				strconv.FormatFloat(st.max, 'f', 0, 64), strconv.FormatFloat(st.reqps, 'f', 2, 64),
				strconv.FormatFloat(st.tokps, 'f', 0, 64), st.firstErr})
		}
		w.Flush()
		return w.Error()
	} else if len(models) > 1 && len(all) > 0 {
		fmt.Printf("\ncomparison (n=%d conc=%d):\n", *n, *conc)
		fmt.Printf("%-24s %5s %8s %8s %8s %9s\n", "MODEL", "OK", "AVG MS", "P50 MS", "P95 MS", "TOK/S")
		for _, st := range all {
			fmt.Printf("%-24s %5d %8.0f %8.0f %8.0f %9.0f\n",
				st.model, st.ok, st.avg, st.p50, st.p95, st.tokps)
		}
	}
	if firstErr != nil && len(all) < len(models) {
		return firstErr
	}
	return nil
}

func cmdComplete(args []string) error {
	fs := flag.NewFlagSet("complete", flag.ExitOnError)
	model := fs.String("model", "", "model id (required)")
	keyF := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled; default $LLMCTL_API_KEY)")
	maxTok := fs.Int("max-tokens", 256, "max tokens to generate")
	temp := fs.Float64("temperature", 0.7, "sampling temperature")
	jsonOut := fs.Bool("json", false, "print the raw completions JSON (for scripts)")
	outFile := fs.String("out", "", "write the completion text to this file instead of stdout (F455)")
	fs.Parse(reorderFlags(args))
	if *model == "" {
		return fmt.Errorf("usage: llmctl complete -model ID [-key K] [-max-tokens N] [-temperature T] [-json] <prompt>  (or pipe stdin)")
	}
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			prompt = strings.TrimSpace(string(b))
		}
	}
	if prompt == "" {
		return fmt.Errorf("no prompt (pass it as an argument or pipe it to stdin)")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.DoKey("POST", "/v1/completions", map[string]any{
		"model":       *model,
		"prompt":      prompt,
		"max_tokens":  *maxTok,
		"temperature": *temp,
	}, *keyF)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("complete: %s", out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	var res struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &res); err != nil || len(res.Choices) == 0 {
		return fmt.Errorf("complete: unexpected response: %s", out)
	}
	if *outFile != "" { // F455: completion text only, for scripts
		text := res.Choices[0].Text
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if err := os.WriteFile(*outFile, []byte(text), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(text))
		return nil
	}
	fmt.Print(res.Choices[0].Text)
	if !strings.HasSuffix(res.Choices[0].Text, "\n") {
		fmt.Println()
	}
	return nil
}

func cmdCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	modelsFlag := fs.String("models", "", "comma-separated model ids or aliases (required)")
	keyF := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled; default $LLMCTL_API_KEY)")
	system := fs.String("system", "", "system prompt prepended to the conversation")
	maxTok := fs.Int("max-tokens", 0, "max tokens per response (0 = model default)")
	temp := fs.Float64("temperature", -1, "sampling temperature (-1 = model default, F343)")
	jsonOut := fs.Bool("json", false, "print [{model, reply} | {model, error}] as one JSON array instead of streamed text (F314)")
	outFile := fs.String("out", "", "write the JSON array to this file instead of stdout (implies -json, F454)")
	fs.Parse(reorderFlags(args))
	if *outFile != "" {
		*jsonOut = true // -out implies the JSON shape
	}
	if *modelsFlag == "" {
		return fmt.Errorf("usage: llmctl compare -models A,B [-key K] [-system S] [-max-tokens N] [-temperature T] <prompt>  (or pipe stdin)")
	}
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			prompt = strings.TrimSpace(string(b))
		}
	}
	if prompt == "" {
		return fmt.Errorf("no prompt (pass it as an argument or pipe it to stdin)")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	type cmpRes struct {
		Model string `json:"model"`
		Reply string `json:"reply,omitempty"`
		Error string `json:"error,omitempty"`
	}
	var results []cmpRes
	for _, m := range strings.Split(*modelsFlag, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		msgs := []map[string]string{}
		if strings.TrimSpace(*system) != "" {
			msgs = append(msgs, map[string]string{"role": "system", "content": strings.TrimSpace(*system)})
		}
		msgs = append(msgs, map[string]string{"role": "user", "content": prompt})
		body := map[string]any{"model": m, "messages": msgs}
		if *maxTok > 0 {
			body["max_tokens"] = *maxTok
		}
		if *temp >= 0 {
			body["temperature"] = *temp
		}
		if !*jsonOut {
			fmt.Printf("=== %s ===\n", m)
		}
		fail := func(msg string) {
			if *jsonOut {
				results = append(results, cmpRes{Model: m, Error: msg})
			} else {
				fmt.Printf("error: %s\n\n", msg)
			}
		}
		body["stream"] = true
		b, _ := json.Marshal(body)
		req, err := http.NewRequest(http.MethodPost, cc.Base+"/v1/chat/completions", strings.NewReader(string(b)))
		if err != nil {
			fail(err.Error())
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if *keyF != "" {
			req.Header.Set("Authorization", "Bearer "+*keyF)
		}
		hc := &http.Client{Timeout: 10 * time.Minute}
		resp, err := hc.Do(req)
		if err != nil {
			fail(err.Error())
			continue
		}
		if resp.StatusCode != 200 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fail(string(bodyBytes))
			continue
		}
		var reply strings.Builder
		rd := bufio.NewReader(resp.Body)
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				break
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
			if json.Unmarshal([]byte(data), &chunk) == nil && len(chunk.Choices) > 0 {
				if !*jsonOut {
					fmt.Print(chunk.Choices[0].Delta.Content)
				}
				reply.WriteString(chunk.Choices[0].Delta.Content)
			}
		}
		resp.Body.Close()
		if *jsonOut {
			results = append(results, cmpRes{Model: m, Reply: reply.String()})
		} else {
			fmt.Print("\n\n")
		}
	}
	if *jsonOut {
		b, _ := json.Marshal(results)
		if *outFile != "" { // F454: JSON array to a file, stdout stays clean
			if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
			return nil
		}
		fmt.Println(string(b))
	}
	return nil
}

// cmdCount estimates tokens for text locally (~4 chars/token, the same
// heuristic the chat context meter uses) — handy for sizing prompts before
// sending them (F317).
// llmTopCommands is the canonical top-level command list, used by both the
// help text and shell completion (F341).
var llmTopCommands = []string{
	"up", "down", "status", "doctor", "gpu", "disk", "presets", "catalog",
	"maxparallel", "jobs", "search", "structured", "embed", "complete",
	"compare", "count", "bench", "note", "autorestart", "setvars", "ps",
	"backends", "models", "start", "stop", "restart", "logs", "key",
	"autostart", "requests", "backups", "usage", "metrics", "events",
	"webhooks", "prompts", "chats", "skills", "memory", "kb", "tools",
	"mcp", "cost", "files", "settings", "batch", "scheduler", "chat",
	"backup", "restore", "version", "help", "completion",
}

// llmSubCommands lists the subcommands of each top-level command that has
// them, used by shell completion (F350). Keys must match llmTopCommands.
var llmSubCommands = map[string]string{
	"autostart": "list add remove export import",
	"backends":  "list install uninstall add remove",
	"backups":   "list create restore verify download delete",
	"catalog":   "list install",
	"chats":     "list show search export export-all import fork rename pin unpin prune delete delete-all",
	"files":     "list upload cat delete",
	"kb":        "list show create update docs doc add-doc export search embed delete",
	"key":       "generate show note enable disable rotate list revoke prune reset-usage",
	"mcp":       "list add remove enable disable test export import",
	"memory":    "list show add update search delete export import",
	"models":    "list show add rename probe price ctx idle metadata install remove routable unused hf-search hf-files aliases alias unalias",
	"presets":   "list save start delete export import",
	"prompts":   "list show render categories add update delete export import",
	"scheduler": "list show add update run runs enable disable delete export import",
	"settings":  "set test-webhook",
	"skills":    "list show add export import enable disable delete",
	"tools":     "list show install enable disable run delete",
}

// cmdCompletion prints a shell completion script (bash or zsh) for the
// top-level llmctl commands and their subcommands (F341, F350, F354).
func cmdCompletion(args []string) error {
	shell := "bash"
	if len(args) > 0 {
		shell = args[0]
	}
	cmds := strings.Join(llmTopCommands, " ")
	// Sorted keys so the generated script is stable.
	names := make([]string, 0, len(llmSubCommands))
	for k := range llmSubCommands {
		names = append(names, k)
	}
	sort.Strings(names)
	switch shell {
	case "bash":
		var sb strings.Builder
		for _, n := range names {
			fmt.Fprintf(&sb, "    %s) subs=\"%s\" ;;\n", n, llmSubCommands[n])
		}
		fmt.Printf(`_llmctl_completions() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
    return
  fi
  local subs=""
  case "${COMP_WORDS[1]}" in
%s  esac
  COMPREPLY=( $(compgen -W "$subs" -- "$cur") )
}
complete -F _llmctl_completions llmctl
`, cmds, sb.String())
	case "zsh":
		quoted := ""
		for _, c := range llmTopCommands {
			quoted += "  '" + c + "'\n"
		}
		var sb strings.Builder
		for _, n := range names {
			subs := strings.Fields(llmSubCommands[n])
			var q []string
			for _, s := range subs {
				q = append(q, "'"+s+"'")
			}
			fmt.Fprintf(&sb, "      %s) subs=(%s) ;;\n", n, strings.Join(q, " "))
		}
		fmt.Printf(`#compdef llmctl
_llmctl() {
  local -a cmds
  cmds=(
%s  )
  if (( CURRENT == 2 )); then
    _describe 'command' cmds
    return
  fi
  local -a subs
  case "${words[2]}" in
%s  esac
  (( ${#subs} > 0 )) && _describe 'subcommand' subs
}
compdef _llmctl llmctl
`, quoted, sb.String())
	case "fish":
		// Fish: one `complete` line per command (level 1) and per command's
		// subcommand set (level 2) (F354).
		var sb strings.Builder
		for _, n := range names {
			fmt.Fprintf(&sb, "complete -c llmctl -n \"__fish_seen_subcommand_from %s\" -a \"%s\"\n", n, llmSubCommands[n])
		}
		fmt.Printf(`complete -c llmctl -f
complete -c llmctl -n __fish_use_subcommand -a "%s"
%s`, cmds, sb.String())
	case "powershell", "pwsh":
		// PowerShell: Register-ArgumentCompleter over the same command and
		// subcommand tables (F425). Add to $PROFILE to activate.
		var ps []string
		for _, c := range llmTopCommands {
			ps = append(ps, "'"+c+"'")
		}
		var sb strings.Builder
		for _, n := range names {
			var q []string
			for _, s := range strings.Fields(llmSubCommands[n]) {
				q = append(q, "'"+s+"'")
			}
			fmt.Fprintf(&sb, "      '%s' { @(%s) }\n", n, strings.Join(q, ", "))
		}
		fmt.Printf(`# llmctl completion for PowerShell — add this to $PROFILE:
#   llmctl completion powershell | Out-String | Invoke-Expression
Register-ArgumentCompleter -CommandName llmctl -ScriptBlock {
  param($WordToComplete, $CommandAst, $CursorPosition)
  $words = @($CommandAst.CommandElements | ForEach-Object { $_.Extent.Text })
  if ($words.Count -le 1 -or ($words.Count -eq 2 -and $WordToComplete)) {
    @(%s) | Where-Object { $_ -like "$WordToComplete*" }
    return
  }
  $subs = switch ($words[1]) {
%s    default { @() }
  }
  $subs | Where-Object { $_ -like "$WordToComplete*" }
}
`, strings.Join(ps, ", "), sb.String())
	default:
		return fmt.Errorf("usage: llmctl completion [bash|zsh|fish|powershell]")
	}
	return nil
}

func cmdCount(args []string) error {
	fs := flag.NewFlagSet("count", flag.ExitOnError)
	file := fs.String("file", "", "read the text from this file")
	chat := fs.String("chat", "", "size a saved chat by id (its system prompt + all messages, F336)")
	model := fs.String("model", "", "also show the share of this model's context window")
	jsonOut := fs.Bool("json", false, "print the counts as JSON (for scripts)")
	outFile := fs.String("out", "", "write the counts JSON to this file instead of stdout (implies -json, F461)")
	fs.Parse(reorderFlags(args))
	text := strings.Join(fs.Args(), " ")
	if text == "" && *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		text = string(b)
	}
	if text == "" {
		if fi, _ := os.Stdin.Stat(); fi.Mode()&os.ModeCharDevice == 0 {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			text = string(b)
		}
	}
	chatMsgs := 0
	if *chat != "" {
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(*chat))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat %s: %s", *chat, out)
		}
		var ch struct {
			System   string `json:"system"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(out, &ch); err != nil {
			return err
		}
		var sb strings.Builder
		if ch.System != "" {
			sb.WriteString(ch.System)
			sb.WriteString("\n")
		}
		for _, m := range ch.Messages {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
			chatMsgs++
		}
		text = sb.String()
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("usage: llmctl count <text> | llmctl count -file F | llmctl count -chat ID | echo … | llmctl count")
	}
	chars := len(text)
	words := len(strings.Fields(text))
	tokens := (chars + 3) / 4
	ctx := 0
	if *model != "" {
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/models/" + url.PathEscape(*model))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("model %s: %s", *model, out)
		}
		var m struct {
			CtxSize int `json:"ctxSize"`
		}
		json.Unmarshal(out, &m)
		ctx = m.CtxSize
	}
	if *jsonOut || *outFile != "" { // -out implies the JSON shape (F461)
		b, _ := json.Marshal(map[string]any{
			"chars": chars, "words": words, "est_tokens": tokens,
			"model": *model, "ctx_tokens": ctx, "chat_id": *chat, "messages": chatMsgs,
		})
		if *outFile != "" { // F461: counts JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
			return nil
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("chars: %d  words: %d  est. tokens: %d (~4 chars/token)\n", chars, words, tokens)
	if *chat != "" {
		fmt.Printf("chat %s: %d message(s)\n", *chat, chatMsgs)
	}
	if ctx > 0 {
		fmt.Printf("model %s context: %d tokens — this text is %.1f%% of it\n",
			*model, ctx, 100*float64(tokens)/float64(ctx))
	} else if *model != "" {
		fmt.Printf("model %s has no context size set (llmctl models ctx %s <tokens>)\n", *model, *model)
	}
	return nil
}

func cmdPS(args []string) error {
	fs := flag.NewFlagSet("ps", flag.ExitOnError)
	watch := fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once)")
	csvOut := fs.Bool("csv", false, "output the instance table as CSV (F373; ignores -watch)")
	jsonOut := fs.Bool("json", false, "print the raw instances JSON (for scripts)")
	outFile := fs.String("out", "", "write the raw instances JSON to this file instead of stdout (F467)")
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
		if *outFile != "" { // F467: latest snapshot to a file (each -watch refresh overwrites)
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			if *watch <= 0 {
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			}
			return nil
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		return printPS(out, *csvOut)
	}
	if *watch <= 0 || *csvOut { // CSV is a one-shot export (F373)
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
		if err := printPS(out, false); err != nil {
			return err
		}
		time.Sleep(time.Duration(*watch) * time.Second)
	}
}

func printPS(out []byte, csvOut bool) error {
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
		if csvOut { // keep the stream parseable (F373)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "model", "backend", "transport", "port", "status", "uptime_s", "served", "restarts", "note"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no instances (start one: llmctl start <model-id>)")
		return nil
	}
	if csvOut { // F373
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "model", "backend", "transport", "port", "status", "uptime_s", "served", "restarts", "note"})
		for _, i := range list {
			up := int64(0)
			if i.Status == "running" && !i.StartedAt.IsZero() {
				up = int64(time.Since(i.StartedAt).Seconds())
			}
			_ = w.Write([]string{i.ID, i.ModelID, i.BackendID, i.Transport, strconv.Itoa(i.Port), i.Status,
				strconv.FormatInt(up, 10), strconv.FormatInt(i.Served, 10), strconv.Itoa(i.RestartCount), i.Note})
		}
		w.Flush()
		return w.Error()
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
		csvOut := fs.Bool("csv", false, "output the backends list as CSV (F388)")
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
		if *csvOut { // F388: raw fields; header-only when empty
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "transport", "kind", "installed", "version", "description"})
			for _, b := range list {
				installed := "false"
				if b["installed"] == true {
					installed = "true"
				}
				ver := ""
				if b["version"] != nil {
					ver = fmt.Sprint(b["version"])
				}
				_ = w.Write([]string{fmt.Sprint(b["id"]), fmt.Sprint(b["transport"]), fmt.Sprint(b["kind"]),
					installed, ver, fmt.Sprint(b["description"])})
			}
			w.Flush()
			return w.Error()
		}
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
		return fmt.Errorf("usage: llmctl models <list|show|add|rename|probe|price|ctx|idle|metadata|install|remove|routable|hf-search|hf-files>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "routable":
		// Show what the gateway advertises to OpenAI-compatible clients
		// (GET /v1/models): running models + aliases, subject to the key's
		// allowlist — debug "my client can't see the model" (F337).
		rfs := flag.NewFlagSet("models routable", flag.ExitOnError)
		keyF := rfs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled)")
		rjson := rfs.Bool("json", false, "print the raw /v1/models JSON")
		rcsv := rfs.Bool("csv", false, "output the advertised models as CSV (F407)")
		rfs.Parse(reorderFlags(rest))
		req, err := http.NewRequest(http.MethodGet, cc.Base+"/v1/models", nil)
		if err != nil {
			return err
		}
		if *keyF != "" {
			req.Header.Set("Authorization", "Bearer "+*keyF)
		}
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return fmt.Errorf("gateway /v1/models returned %d: %s", resp.StatusCode, string(body))
		}
		if *rjson {
			fmt.Print(string(body))
			return nil
		}
		var parsed struct {
			Data []struct {
				ID      string `json:"id"`
				OwnedBy string `json:"owned_by"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return err
		}
		if *rcsv { // F407: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "owned_by"})
			for _, m := range parsed.Data {
				_ = w.Write([]string{m.ID, m.OwnedBy})
			}
			w.Flush()
			return w.Error()
		}
		if len(parsed.Data) == 0 {
			fmt.Println("gateway advertises no models (start an instance or check the key's allowlist)")
			return nil
		}
		fmt.Printf("gateway advertises %d model(s):\n", len(parsed.Data))
		for _, m := range parsed.Data {
			fmt.Printf("  %-24s %s\n", m.ID, m.OwnedBy)
		}
		return nil
	case "rename":
		if len(rest) < 2 {
			return fmt.Errorf("usage: llmctl models rename <id> <new-id>")
		}
		code, out, err := cc.Do("PATCH", "/api/v1/models/"+url.PathEscape(rest[0])+"/rename",
			map[string]any{"newId": rest[1]})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models rename: %s", out)
		}
		fmt.Printf("model %s renamed to %s\n", rest[0], rest[1])
		return nil
	case "add":
		afs := flag.NewFlagSet("models add", flag.ExitOnError)
		backendID := afs.String("backend", "", "backend id (default: llama.cpp)")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl models add <id> <file> [-backend ID]")
		}
		body := map[string]any{"id": afs.Arg(0), "path": afs.Arg(1)}
		if *backendID != "" {
			body["backend"] = *backendID
		}
		code, out, err := cc.Post("/api/v1/models", body)
		if err != nil {
			return err
		}
		if code != 200 && code != 201 {
			return fmt.Errorf("models add: %s", out)
		}
		fmt.Printf("model %s registered from %s\n", afs.Arg(0), afs.Arg(1))
		return nil
	case "unused":
		// Models with no running instance and no stored instance record —
		// cleanup candidates (F280).
		uufs := flag.NewFlagSet("models unused", flag.ExitOnError)
		uucsv := uufs.Bool("csv", false, "output the unused models as CSV (F408)")
		uufs.Parse(reorderFlags(rest))
		_, mout, err := cc.Get("/api/v1/models")
		if err != nil {
			return err
		}
		_, iout, err := cc.Get("/api/v1/instances")
		if err != nil {
			return err
		}
		var models []struct {
			ID      string `json:"id"`
			Size    int64  `json:"sizeBytes"`
			Running bool   `json:"running"`
		}
		var insts []struct {
			ModelID string `json:"modelId"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(mout, &models); err != nil {
			return err
		}
		if err := json.Unmarshal(iout, &insts); err != nil {
			return err
		}
		used := map[string]bool{}
		for _, i := range insts {
			used[i.ModelID] = true
		}
		var unused []struct {
			ID   string
			Size int64
		}
		for _, m := range models {
			if m.Running || used[m.ID] {
				continue
			}
			unused = append(unused, struct {
				ID   string
				Size int64
			}{m.ID, m.Size})
		}
		if *uucsv { // F408: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "size_bytes"})
			for _, m := range unused {
				_ = w.Write([]string{m.ID, strconv.FormatInt(m.Size, 10)})
			}
			w.Flush()
			return w.Error()
		}
		var total int64
		for _, m := range unused {
			fmt.Printf("%-24s %-12s %s\n", m.ID, fmtSize(m.Size), "llmctl models remove "+m.ID)
			total += m.Size
		}
		if len(unused) == 0 {
			fmt.Println("every model has a running or stored instance")
			return nil
		}
		fmt.Printf("total: %d unreferenced model(s), %s reclaimable\n", len(unused), fmtSize(total))
		return nil
	case "list":
		fs := flag.NewFlagSet("models list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw models JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the model table as CSV (F372)")
		outFile := fs.String("out", "", "write the raw models JSON to this file instead of stdout (F479)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/models")
		if err != nil {
			return err
		}
		if *outFile != "" { // F479: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
			if *csvOut { // keep the stream parseable (F372)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "backend", "size_bytes", "ctx_size", "price_in_per_m", "price_out_per_m", "idle_unload_min", "running"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no models downloaded")
			return nil
		}
		if *csvOut { // F372
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "backend", "size_bytes", "ctx_size", "price_in_per_m", "price_out_per_m", "idle_unload_min", "running"})
			for _, m := range list {
				_ = w.Write([]string{m.ID, m.BackendID, strconv.FormatInt(m.SizeBytes, 10), strconv.Itoa(m.CtxSize),
					strconv.FormatFloat(m.PriceIn, 'f', -1, 64), strconv.FormatFloat(m.PriceOut, 'f', -1, 64),
					strconv.Itoa(m.IdleMin), strconv.FormatBool(m.Running)})
			}
			w.Flush()
			return w.Error()
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
	case "metadata":
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl models metadata <id> [-json]")
		}
		mfs := flag.NewFlagSet("models metadata", flag.ExitOnError)
		mjson := mfs.Bool("json", false, "print the raw metadata JSON (for scripts)")
		mfs.Parse(reorderFlags(rest[1:]))
		code, out, err := cc.Get("/api/v1/models/" + url.PathEscape(rest[0]) + "/metadata")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("metadata for %s: %s", rest[0], out)
		}
		if *mjson {
			fmt.Print(string(out))
			return nil
		}
		var md struct {
			ID       string         `json:"id"`
			Path     string         `json:"path"`
			Version  int            `json:"version"`
			NTensors int            `json:"nTensors"`
			Summary  map[string]any `json:"summary"`
		}
		if err := json.Unmarshal(out, &md); err != nil {
			return err
		}
		fmt.Printf("id:       %s\npath:     %s\ngguf:     v%d, %d tensors\n", md.ID, md.Path, md.Version, md.NTensors)
		keys := make([]string, 0, len(md.Summary))
		for k := range md.Summary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%-10s %v\n", k+":", md.Summary[k])
		}
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
		csvOut := fs.Bool("csv", false, "output the alias list as CSV (F385)")
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
		if *csvOut { // F385: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"alias", "model"})
			for _, a := range als {
				_ = w.Write([]string{a.Alias, a.Model})
			}
			w.Flush()
			return w.Error()
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
		sout := sfs.String("out", "", "write the raw model JSON to this file instead of stdout (F480)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models show <id> [-json] [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/models/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models show returned %d: %s", code, out)
		}
		if *sout != "" { // F480: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*sout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *sout, len(out))
			return nil
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
			Aliases       []string `json:"aliases"`
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
		if len(mv.Aliases) > 0 {
			fmt.Printf("aliases:  %s\n", strings.Join(mv.Aliases, ", "))
		}
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
	all := fs.Bool("all", false, "stop every running instance (F278)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 && !*all {
		return fmt.Errorf("usage: llmctl stop <instance> [--force] | llmctl stop --all")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	if *all {
		code, out, err := cc.Post("/api/v1/instances/stop-all", nil)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("stop --all returned %d: %s", code, out)
		}
		fmt.Println(string(out))
		return nil
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
	fs := flag.NewFlagSet("restart", flag.ExitOnError)
	all := fs.Bool("all", false, "restart every running instance (F445)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 && !*all {
		return fmt.Errorf("usage: llmctl restart <instance> | llmctl restart --all")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	if *all { // F445: restart each running instance, mirroring stop --all
		code, out, err := cc.Get("/api/v1/instances")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("instances returned %d: %s", code, out)
		}
		var list []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		var attempted int
		for _, inst := range list {
			if inst.Status != "running" {
				continue
			}
			attempted++
			code, out, err := cc.Post("/api/v1/instances/"+url.PathEscape(inst.ID)+"/restart", nil)
			if err != nil {
				return err
			}
			if code != 200 {
				fmt.Printf("restart %s failed: %s\n", inst.ID, strings.TrimSpace(string(out)))
				continue
			}
			fmt.Printf("restarted %s\n", inst.ID)
		}
		if attempted == 0 {
			fmt.Println("no running instances")
		}
		return nil
	}
	code, out, err := cc.Post("/api/v1/instances/"+url.PathEscape(fs.Arg(0))+"/restart", nil)
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
	outFile := fs.String("out", "", "with an instance: download the full log file to this path (F292)")
	follow := fs.Bool("f", false, "keep printing new log bytes until Ctrl-C (F316)")
	interval := fs.Int("interval", 1, "with -f: poll every N seconds")
	fs.Parse(reorderFlags(args))
	if *daemonFlag {
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		fetch := func() (int64, string, error) {
			code, out, err := cc.Get(fmt.Sprintf("/api/v1/logs/daemon?bytes=%d", *tail))
			if err != nil {
				return 0, "", err
			}
			if code != 200 {
				return 0, "", fmt.Errorf("daemon log returned %d: %s", code, out)
			}
			var v struct {
				Size int64  `json:"size"`
				Logs string `json:"logs"`
			}
			if err := json.Unmarshal(out, &v); err != nil {
				return 0, "", err
			}
			return v.Size, v.Logs, nil
		}
		size, logs, err := fetch()
		if err != nil {
			return err
		}
		fmt.Print(logs)
		if !*follow {
			return nil
		}
		// Follow: poll and print only bytes appended since the last read;
		// a shrinking size means the log was rotated/truncated (F355).
		seen := size
		for {
			time.Sleep(time.Duration(*interval) * time.Second)
			size, logs, err := fetch()
			if err != nil {
				continue
			}
			if size < seen {
				seen = 0
			}
			if size > seen {
				off := seen - (size - int64(len(logs)))
				if off < 0 {
					off = 0
				}
				fmt.Print(logs[off:])
				seen = size
			}
		}
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl logs <instance> [--stream stderr|stdout] [--tail N] [-out FILE] [-f] | llmctl logs -daemon [--tail N] [-f]")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	if *outFile != "" {
		// Full log file as an attachment (GET …/logs/download, F292).
		code, body, err := cc.Get("/api/v1/instances/" + url.PathEscape(fs.Arg(0)) + "/logs/download?stream=" + url.QueryEscape(*stream))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("logs download: %s", body)
		}
		if err := os.WriteFile(*outFile, body, 0o644); err != nil {
			return err
		}
		fmt.Printf("downloaded %s %s log to %s (%s)\n", fs.Arg(0), *stream, *outFile, fmtSize(int64(len(body))))
		return nil
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
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return err
	}
	fmt.Print(v.Logs)
	if !*follow {
		return nil
	}
	// Follow: the API reports the total file size, so we only print the
	// bytes that appeared since the last poll (F316).
	seen := v.Size
	for {
		time.Sleep(time.Duration(*interval) * time.Second)
		code, out, err := cc.Get(fmt.Sprintf("/api/v1/instances/%s/logs?stream=%s&bytes=%d",
			fs.Arg(0), url.QueryEscape(*stream), 1<<20))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("logs returned %d: %s", code, out)
		}
		var nv struct {
			Logs string `json:"logs"`
			Size int64  `json:"size"`
		}
		if err := json.Unmarshal(out, &nv); err != nil {
			return err
		}
		if nv.Size < seen {
			seen = 0 // log truncated or rotated: reprint from the start
		}
		if nv.Size > seen {
			n := nv.Size - seen
			if int64(len(nv.Logs)) >= n {
				fmt.Print(nv.Logs[int64(len(nv.Logs))-n:])
			} else {
				fmt.Print(nv.Logs) // grew faster than one fetch window
			}
			seen = nv.Size
		}
	}
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
		return fmt.Errorf("usage: llmctl scheduler <list|show|add|update|run|enable|disable|delete|export|import> [id] [flags]")
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
	case "export":
		// Dump all schedule specs as a JSON array (feed to `scheduler import`, F326).
		efs := flag.NewFlagSet("scheduler export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/scheduler")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler export: %s", out)
		}
		var list []struct {
			Spec json.RawMessage `json:"spec"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		specs := []json.RawMessage{}
		for _, r := range list {
			specs = append(specs, r.Spec)
		}
		data, err := json.MarshalIndent(specs, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %d schedules to %s\n", len(specs), *outFile)
			return nil
		}
		fmt.Print(string(data))
		return nil
	case "import":
		// Create schedules from a `scheduler export` file (F326). Each
		// import creates fresh ids; re-running duplicates rather than merges.
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl scheduler import <file.json>")
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		var specs []map[string]any
		if err := json.Unmarshal(data, &specs); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		var created, skipped int
		for _, spec := range specs {
			delete(spec, "id") // the store mints its own ids
			code, out, err := cc.Post("/api/v1/scheduler", spec)
			if err != nil {
				return err
			}
			if code != 201 {
				fmt.Printf("  skip: %s\n", out)
				skipped++
				continue
			}
			created++
		}
		fmt.Printf("imported %d schedules (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("scheduler list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw scheduler JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the schedule table as CSV (F376)")
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
			if *csvOut { // keep the stream parseable (F376)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "name", "kind", "cron", "model", "enabled", "next_run", "last_run_time", "last_run_status"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no scheduled tasks")
			return nil
		}
		if *csvOut { // F376
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "kind", "cron", "model", "enabled", "next_run", "last_run_time", "last_run_status"})
			for _, r := range recs {
				kind := r.Spec.Kind
				if kind == "" {
					kind = "chat"
				}
				lt, ls := "", ""
				if r.LastRun != nil {
					lt, ls = r.LastRun.Time, r.LastRun.Status
				}
				_ = w.Write([]string{r.ID, r.Spec.Name, kind, r.Spec.CronExpr, r.Spec.Model,
					strconv.FormatBool(r.Spec.Enabled), r.Spec.NextRun, lt, ls})
			}
			w.Flush()
			return w.Error()
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
		kb := fs.String("kb", "", "knowledge base id: top matching chunks are injected as context (chat kind)")
		fs.Parse(reorderFlags(rest))
		body := map[string]any{
			"name": *name, "cron_expr": *cron, "kind": *kind,
			"model": *model, "prompt": *prompt, "instance": *instance,
			"enabled": true,
		}
		if *kb != "" {
			body["kb"] = *kb
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
	case "update":
		// Read-modify-write: fetch the current spec, apply the flags given,
		// PUT the whole spec back (F290).
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl scheduler update <id> [-name N] [-cron C] [-model M] [-prompt P] [-instance I] [-webhook U] [-keep N] [-enabled true|false] [-kb ID]")
		}
		id := rest[0]
		ufs := flag.NewFlagSet("scheduler update", flag.ExitOnError)
		name := ufs.String("name", "", "task name")
		cron := ufs.String("cron", "", "cron expression")
		model := ufs.String("model", "", "model id")
		prompt := ufs.String("prompt", "", "prompt (chat kind)")
		instance := ufs.String("instance", "", "instance id (instance.stop)")
		webhook := ufs.String("webhook", "", "webhook URL (empty string clears)")
		keep := ufs.Int("keep", -1, "snapshots to retain (backup kind)")
		enabled := ufs.String("enabled", "", "true | false")
		kb := ufs.String("kb", "", "knowledge base id (chat kind)")
		ufs.Parse(reorderFlags(rest[1:]))
		code, out, err := cc.Get("/api/v1/scheduler/" + url.PathEscape(id))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler update: %s", out)
		}
		var rec struct {
			Spec map[string]any `json:"spec"`
		}
		if err := json.Unmarshal(out, &rec); err != nil || rec.Spec == nil {
			return fmt.Errorf("scheduler update: unexpected record: %s", out)
		}
		if *name != "" {
			rec.Spec["name"] = *name
		}
		if *cron != "" {
			rec.Spec["cron_expr"] = *cron
		}
		if *model != "" {
			rec.Spec["model"] = *model
		}
		if *prompt != "" {
			rec.Spec["prompt"] = *prompt
		}
		if *instance != "" {
			rec.Spec["instance"] = *instance
		}
		if *webhook != "" {
			rec.Spec["webhook_url"] = *webhook
		}
		if *keep >= 0 {
			rec.Spec["keep"] = *keep
		}
		if *kb != "" {
			rec.Spec["kb"] = *kb
		}
		switch *enabled {
		case "":
		case "true":
			rec.Spec["enabled"] = true
		case "false":
			rec.Spec["enabled"] = false
		default:
			return fmt.Errorf("-enabled must be true or false")
		}
		if *name == "" && *cron == "" && *model == "" && *prompt == "" && *instance == "" && *webhook == "" && *keep < 0 && *enabled == "" && *kb == "" {
			return fmt.Errorf("nothing to change (pass at least one flag)")
		}
		delete(rec.Spec, "id")
		delete(rec.Spec, "dir")
		code, out, err = cc.Do("PUT", "/api/v1/scheduler/"+url.PathEscape(id), rec.Spec)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler update: %s", out)
		}
		fmt.Printf("scheduler task %s updated\n", id)
		return nil
	case "runs":
		fs := flag.NewFlagSet("scheduler runs", flag.ExitOnError)
		n := fs.Int("n", 20, "number of recent runs")
		jsonOut := fs.Bool("json", false, "print the run log as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the run log as CSV (F418)")
		outFile := fs.String("out", "", "write the run log JSON to this file instead of stdout (F476)")
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
		if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F476)
			if len(runs) > *n {
				runs = runs[:*n]
			}
			payload := []byte("[]\n")
			if len(runs) > 0 {
				b, err := json.Marshal(runs)
				if err != nil {
					return err
				}
				payload = append(b, '\n')
			}
			if *outFile != "" {
				if err := os.WriteFile(*outFile, payload, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(payload))
				return nil
			}
			fmt.Print(string(payload))
			return nil
		}
		if *csvOut { // F418: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"time", "name", "kind", "status", "duration_ms", "detail"})
			for _, r := range runs {
				_ = w.Write([]string{r.Time.UTC().Format(time.RFC3339), r.Name, r.Kind, r.Status,
					strconv.FormatInt(r.DurationMs, 10), r.Detail})
			}
			w.Flush()
			return w.Error()
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
	outFile := fs.String("out", "", "write the raw requests JSON to this file instead of stdout (F473)")
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
		if *outFile != "" { // F473: single request JSON to a file
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
	if (*jsonOut || *outFile != "") && !*stats { // -out writes the same payload to a file (F473)
		if *outFile != "" {
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
		}
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
			if *csvOut { // F420: one row per group, header-only when empty
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"group", "requests", "errors", "error_pct", "avg_ms", "p50_ms", "p95_ms", "max_ms", "in_tokens", "out_tokens", "cost_usd"})
				for _, g := range names {
					a := statsOf(groups[g])
					_ = w.Write([]string{g, strconv.Itoa(a.Requests), strconv.Itoa(a.Errors),
						strconv.FormatFloat(a.ErrorPct, 'f', 1, 64), strconv.FormatFloat(a.AvgMs, 'f', 0, 64),
						strconv.FormatFloat(a.P50Ms, 'f', 0, 64), strconv.FormatFloat(a.P95Ms, 'f', 0, 64),
						strconv.FormatFloat(a.MaxMs, 'f', 0, 64), strconv.FormatInt(a.In, 10),
						strconv.FormatInt(a.Out, 10), strconv.FormatFloat(a.Cost, 'f', 4, 64)})
				}
				w.Flush()
				return w.Error()
			}
			fmt.Printf("%-20s %7s %7s %8s %8s %10s %10s %9s\n", strings.ToUpper(*by), "REQS", "ERRORS", "P50 MS", "P95 MS", "IN", "OUT", "COST $")
			for _, g := range names {
				a := statsOf(groups[g])
				fmt.Printf("%-20s %7d %4d %3.0f%% %8.0f %8.0f %10d %10d %9.4f\n", g, a.Requests, a.Errors, a.ErrorPct, a.P50Ms, a.P95Ms, a.In, a.Out, a.Cost)
			}
			return nil
		}
		a := statsOf(entries)
		if *csvOut { // F420: single aggregate row
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"group", "requests", "errors", "error_pct", "avg_ms", "p50_ms", "p95_ms", "max_ms", "in_tokens", "out_tokens", "cost_usd"})
			_ = w.Write([]string{"(all)", strconv.Itoa(a.Requests), strconv.Itoa(a.Errors),
				strconv.FormatFloat(a.ErrorPct, 'f', 1, 64), strconv.FormatFloat(a.AvgMs, 'f', 0, 64),
				strconv.FormatFloat(a.P50Ms, 'f', 0, 64), strconv.FormatFloat(a.P95Ms, 'f', 0, 64),
				strconv.FormatFloat(a.MaxMs, 'f', 0, 64), strconv.FormatInt(a.In, 10),
				strconv.FormatInt(a.Out, 10), strconv.FormatFloat(a.Cost, 'f', 4, 64)})
			w.Flush()
			return w.Error()
		}
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
		csvOut := fs.Bool("csv", false, "output the backup list as CSV (F383)")
		outFile := fs.String("out", "", "write the raw backups JSON to this file instead of stdout (F475)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/backups")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backups list returned %d: %s", code, out)
		}
		if *outFile != "" { // F475: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
		if *csvOut { // F383: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"name", "size_bytes", "created"})
			for _, b := range bs {
				_ = w.Write([]string{b.Name, strconv.FormatInt(b.Size, 10), b.Created})
			}
			w.Flush()
			return w.Error()
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
		cfs := flag.NewFlagSet("backups create", flag.ExitOnError)
		label := cfs.String("name", "", "label embedded in the backup file name (F443)")
		cfs.Parse(reorderFlags(rest))
		var body any
		if *label != "" {
			body = map[string]string{"label": *label}
		}
		code, out, err := cc.Post("/api/v1/backups", body)
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
	case "download":
		dfs := flag.NewFlagSet("backups download", flag.ExitOnError)
		outFile := dfs.String("out", "", "write to this file (default: the backup name)")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backups download <name> [-out file]")
		}
		name := dfs.Arg(0)
		dest := *outFile
		if dest == "" {
			dest = name
		}
		code, body, err := cc.Get("/api/v1/backups/" + url.PathEscape(name))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backup download: %s", body)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return err
		}
		fmt.Printf("downloaded %s to %s (%s)\n", name, dest, fmtSize(int64(len(body))))
		return nil
	case "delete":
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
	modelF := fs.String("model", "", "show only this model id in the table (F348)")
	csvOut := fs.Bool("csv", false, "output the per-model table as CSV (F370; ignores -watch)")
	jsonOut := fs.Bool("json", false, "print the raw metrics JSON (for scripts)")
	outFile := fs.String("out", "", "write the raw metrics JSON to this file instead of stdout (F469)")
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
		if *outFile != "" { // F469: latest snapshot to a file (each -watch refresh overwrites)
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			if *watch <= 0 {
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			}
			return nil
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		return printMetrics(out, *modelF, *csvOut)
	}
	if *watch <= 0 || *csvOut { // CSV is a one-shot export (F370)
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
		if err := printMetrics(out, *modelF, false); err != nil {
			return err
		}
		time.Sleep(time.Duration(*watch) * time.Second)
	}
}

func printMetrics(out []byte, modelFilter string, csvOut bool) error {
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
	if !csvOut { // CSV output is header + rows only (F370)
		fmt.Printf("requests: %d   tokens: %d (in %d / out %d)\n", m.RequestsTotal, m.Tokens.Total, m.Tokens.Prompt, m.Tokens.Completion)
		classes := make([]string, 0, len(m.Status))
		for k, v := range m.Status {
			classes = append(classes, fmt.Sprintf("%s=%d", k, v))
		}
		sort.Strings(classes)
		fmt.Printf("status: %s\n", strings.Join(classes, " "))
	}
	if len(m.Models) == 0 {
		if csvOut { // F370: keep the stream parseable even when empty
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"model", "requests", "errors", "prompt_tokens", "completion_tokens", "p50_ms", "p95_ms", "tokens_per_sec"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no model traffic yet")
		return nil
	}
	names := make([]string, 0, len(m.Models))
	for k := range m.Models {
		names = append(names, k)
	}
	sort.Strings(names)
	if modelFilter != "" {
		var kept []string
		for _, n := range names {
			if strings.EqualFold(n, modelFilter) {
				kept = append(kept, n)
			}
		}
		if len(kept) == 0 {
			fmt.Printf("no traffic for model %s yet\n", modelFilter)
			return nil
		}
		names = kept
	}
	if csvOut { // F370
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"model", "requests", "errors", "prompt_tokens", "completion_tokens", "p50_ms", "p95_ms", "tokens_per_sec"})
		for _, n := range names {
			v := m.Models[n]
			_ = w.Write([]string{n, strconv.FormatInt(v.Requests, 10), strconv.FormatInt(v.Errors, 10),
				strconv.FormatInt(v.Prompt, 10), strconv.FormatInt(v.Comp, 10),
				strconv.FormatFloat(v.P50, 'f', 0, 64), strconv.FormatFloat(v.P95, 'f', 0, 64),
				strconv.FormatFloat(v.Tps, 'f', 1, 64)})
		}
		w.Flush()
		return w.Error()
	}
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
	outFile := fs.String("out", "", "write the usage JSON to this file instead of stdout (F474)")
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
		if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F474)
			if *outFile != "" {
				if err := os.WriteFile(*outFile, out, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
				return nil
			}
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
		if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F474)
			if *outFile != "" {
				if err := os.WriteFile(*outFile, out, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
				return nil
			}
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
	if (*jsonOut || *outFile != "") && *modelFlag == "" { // -out writes the same payload to a file (F474)
		if *outFile != "" {
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
		}
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
		if *jsonOut || *outFile != "" { // -out writes the filtered payload to a file (F474)
			b, err := json.Marshal(daily)
			if err != nil {
				return err
			}
			if *outFile != "" {
				if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
				return nil
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
	model := fs.String("model", "", "only events for this model id (F345)")
	watch := fs.Int("watch", 0, "poll every N seconds and print new events until Ctrl-C (0 = print once)")
	jsonOut := fs.Bool("json", false, "print the raw events JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	outFile := fs.String("out", "", "write the raw events JSON to this file instead of stdout (F468)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	u := "/api/v1/events?limit=" + strconv.Itoa(*n)
	if *kind != "" {
		u += "&kind=" + url.QueryEscape(*kind)
	}
	if *model != "" {
		u += "&model=" + url.QueryEscape(*model)
	}
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F468)
		code, out, err := cc.Get(u)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("events returned %d: %s", code, out)
		}
		if *outFile != "" {
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		} else {
			fmt.Print(string(out))
		}
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
	if len(args) > 0 && args[0] == "test" { // F447: discoverable twin of settings test-webhook
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
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
	fs := flag.NewFlagSet("webhooks", flag.ExitOnError)
	n := fs.Int("n", 20, "number of deliveries")
	event := fs.String("event", "", "only deliveries for this event name, e.g. instance.crashed (F357)")
	csvOut := fs.Bool("csv", false, "output the delivery log as CSV (F380)")
	jsonOut := fs.Bool("json", false, "print the delivery log as JSON (for scripts)")
	outFile := fs.String("out", "", "write the delivery log JSON to this file instead of stdout (F478)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	path := "/api/v1/webhook/log?limit=" + strconv.Itoa(*n)
	if *event != "" {
		path += "&event=" + url.QueryEscape(*event)
	}
	code, out, err := cc.Get(path)
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
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F478)
		b, err := json.Marshal(list)
		if err != nil {
			return err
		}
		if *outFile != "" {
			b = append(b, '\n')
			if err := os.WriteFile(*outFile, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
			return nil
		}
		fmt.Println(string(b))
		return nil
	}
	if len(list) == 0 {
		if *csvOut { // keep the stream parseable (F380)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"time", "event", "url", "status", "ok", "error"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no webhook deliveries yet")
		return nil
	}
	if *csvOut { // F380
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"time", "event", "url", "status", "ok", "error"})
		for _, d := range list {
			status := ""
			if d.Status > 0 {
				status = strconv.Itoa(d.Status)
			}
			_ = w.Write([]string{d.Time.UTC().Format(time.RFC3339), d.Event, d.URL, status,
				strconv.FormatBool(d.OK), d.Error})
		}
		w.Flush()
		return w.Error()
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
		last := sfs.Int("last", 0, "show only the last N messages (0 = all)")
		grep := sfs.String("grep", "", "show only messages whose content contains this (case-insensitive)")
		full := sfs.Bool("full", false, "print full message text (no 100-char preview truncation)")
		sout := sfs.String("out", "", "write the raw chat JSON to this file instead of stdout (F472)")
		sfs.Parse(reorderFlags(args[1:]))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats show <id> [-json] [-last N] [-grep TEXT] [-full] [-out FILE]")
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
		if *sout != "" { // F472: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*sout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *sout, len(out))
			return nil
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
		msgs := ch.Messages
		if *grep != "" {
			g := strings.ToLower(*grep)
			var kept []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			for _, m := range msgs {
				if strings.Contains(strings.ToLower(m.Content), g) {
					kept = append(kept, m)
				}
			}
			msgs = kept
		}
		if *last > 0 && len(msgs) > *last {
			msgs = msgs[len(msgs)-*last:]
		}
		for _, m := range msgs {
			c := strings.TrimSpace(m.Content)
			if !*full && len(c) > 100 {
				c = c[:100] + "…"
			}
			fmt.Printf("[%s] %s\n", m.Role, strings.ReplaceAll(c, "\n", " "))
		}
		return nil
	}
	if len(args) > 0 && args[0] == "search" {
		qfs := flag.NewFlagSet("chats search", flag.ExitOnError)
		qjson := qfs.Bool("json", false, "print the raw search JSON (for scripts)")
		qout := qfs.String("out", "", "write the raw search JSON to this file instead of stdout (F482)")
		qfs.Parse(reorderFlags(args[1:]))
		if qfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats search <query> [-json] [-out FILE]")
		}
		q := strings.Join(qfs.Args(), " ")
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/chats/search?q=" + url.QueryEscape(q))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chats search: %s", out)
		}
		if *qout != "" { // F482: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*qout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *qout, len(out))
			return nil
		}
		if *qjson {
			fmt.Print(string(out))
			return nil
		}
		var res []struct {
			ConversationID string `json:"conversationId"`
			Title          string `json:"title"`
			Model          string `json:"model"`
			UpdatedAt      string `json:"updatedAt"`
			Matches        []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"matches"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		if len(res) == 0 {
			fmt.Printf("no chats match %q\n", q)
			return nil
		}
		for _, r := range res {
			fmt.Printf("%s  %s  [%s]  %d match(es)\n", r.ConversationID, r.Title, r.Model, len(r.Matches))
			for _, m := range r.Matches {
				sn := strings.TrimSpace(m.Content)
				sn = strings.ReplaceAll(sn, "\n", " ")
				if len(sn) > 90 {
					sn = sn[:90] + "…"
				}
				fmt.Printf("    [%s] %s\n", m.Role, sn)
			}
		}
		return nil
	}
	if len(args) > 0 && args[0] == "export-all" {
		// Bulk export every chat to a directory (F325); re-import happens
		// through `llmctl backup` or per-chat POSTs.
		xfs := flag.NewFlagSet("chats export-all", flag.ExitOnError)
		outDir := xfs.String("out", "", "target directory (default ./chats-export)")
		mdAll := xfs.Bool("md", false, "export as Markdown instead of JSON")
		xfs.Parse(reorderFlags(args[1:]))
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/chats")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chats list: %s", out)
		}
		var chats []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(out, &chats); err != nil {
			return err
		}
		if len(chats) == 0 {
			fmt.Println("no chats to export")
			return nil
		}
		dir := *outDir
		if dir == "" {
			dir = "chats-export"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		ext := "json"
		q := ""
		if *mdAll {
			ext = "md"
			q = "?format=md"
		}
		var n int
		for _, c := range chats {
			code, body, err := cc.Get("/api/v1/chats/" + url.PathEscape(c.ID) + "/export" + q)
			if err != nil {
				return err
			}
			if code != 200 {
				fmt.Printf("  skip %s: %s\n", c.ID, body)
				continue
			}
			name := c.ID + "." + ext
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
				return err
			}
			n++
		}
		fmt.Printf("exported %d chat(s) to %s/\n", n, dir)
		return nil
	}
	if len(args) > 0 && args[0] == "export" {
		efs := flag.NewFlagSet("chats export", flag.ExitOnError)
		md := efs.Bool("md", false, "export as Markdown instead of JSON")
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(args[1:]))
		if efs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats export <id> [-md] [-out file]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		path := "/api/v1/chats/" + url.PathEscape(efs.Arg(0)) + "/export"
		if *md {
			path += "?format=md"
		}
		code, out, err := cc.Get(path)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat export: %s", out)
		}
		if *outFile != "" {
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %s to %s (%s)\n", efs.Arg(0), *outFile, fmtSize(int64(len(out))))
			return nil
		}
		fmt.Print(string(out))
		return nil
	}
	if len(args) > 0 && args[0] == "import" {
		// Re-import a `chats export` JSON file as a NEW chat (F368): the id
		// is dropped so the store mints a fresh one, same convention as
		// scheduler import (F326).
		ifs := flag.NewFlagSet("chats import", flag.ExitOnError)
		jsonOut := ifs.Bool("json", false, "print the saved chat JSON (for scripts)")
		ifs.Parse(reorderFlags(args[1:]))
		if ifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats import <file.json> [-json]")
		}
		b, err := os.ReadFile(ifs.Arg(0))
		if err != nil {
			return err
		}
		var conv map[string]any
		if err := json.Unmarshal(b, &conv); err != nil {
			return fmt.Errorf("import: %w", err)
		}
		delete(conv, "id")
		delete(conv, "context") // recomputed server-side
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Post("/api/v1/chats", conv)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat import: %s", out)
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		var saved struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		json.Unmarshal(out, &saved)
		fmt.Printf("imported as chat %s (%s)\n", saved.ID, saved.Title)
		return nil
	}
	if len(args) > 0 && args[0] == "fork" {
		ffs := flag.NewFlagSet("chats fork", flag.ExitOnError)
		at := ffs.Int("at", 0, "fork after this message index (0 = whole chat)")
		title := ffs.String("title", "", "title for the new chat")
		ffs.Parse(reorderFlags(args[1:]))
		if ffs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats fork <id> [-at N] [-title T]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		body := map[string]any{"forkAt": *at}
		if *title != "" {
			body["title"] = *title
		}
		code, out, err := cc.Post("/api/v1/chats/"+url.PathEscape(ffs.Arg(0))+"/fork", body)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("chat fork: %s", out)
		}
		var ch struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		json.Unmarshal(out, &ch)
		fmt.Printf("forked %s → %s (%s)\n", ffs.Arg(0), ch.ID, ch.Title)
		return nil
	}
	if len(args) > 0 && args[0] == "rename" {
		rfs := flag.NewFlagSet("chats rename", flag.ExitOnError)
		rfs.Parse(reorderFlags(args[1:]))
		if rfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl chats rename <id> <new title>")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(rfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat %s: %s", rfs.Arg(0), out)
		}
		var conv map[string]any
		if err := json.Unmarshal(out, &conv); err != nil {
			return err
		}
		conv["title"] = strings.Join(rfs.Args()[1:], " ")
		code, out, err = cc.Put("/api/v1/chats/"+url.PathEscape(rfs.Arg(0)), conv)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat rename: %s", out)
		}
		fmt.Printf("renamed chat %s to %q\n", rfs.Arg(0), conv["title"])
		return nil
	}
	if len(args) > 0 && args[0] == "prune" {
		// Bulk-delete old saved chats (F333). Dry-run unless -yes; pinned
		// chats are never pruned.
		pfs := flag.NewFlagSet("chats prune", flag.ExitOnError)
		before := pfs.String("before", "", "prune chats last updated before this date (YYYY-MM-DD)")
		older := pfs.Int("older-than", 0, "prune chats last updated more than N days ago")
		yes := pfs.Bool("yes", false, "actually delete (default is a dry run)")
		pfs.Parse(reorderFlags(args[1:]))
		if *before == "" && *older == 0 {
			return fmt.Errorf("refusing to prune everything — pass -before YYYY-MM-DD and/or -older-than N")
		}
		var cutoff time.Time
		if *before != "" {
			t, err := time.Parse("2006-01-02", *before)
			if err != nil {
				return fmt.Errorf("-before: %w", err)
			}
			cutoff = t
		}
		if *older > 0 {
			c := time.Now().AddDate(0, 0, -*older)
			if cutoff.IsZero() || c.After(cutoff) {
				cutoff = c
			}
		}
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
		type chatRow struct {
			ID        string    `json:"id"`
			Title     string    `json:"title"`
			Pinned    bool      `json:"pinned"`
			UpdatedAt time.Time `json:"updatedAt"`
		}
		var list []chatRow
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		var doomed []chatRow
		var skippedPinned int
		for _, c := range list {
			if !c.UpdatedAt.Before(cutoff) {
				continue
			}
			if c.Pinned {
				skippedPinned++
				continue
			}
			doomed = append(doomed, c)
		}
		if len(doomed) == 0 {
			fmt.Printf("no chats older than %s (pinned skipped: %d)\n", cutoff.Format("2006-01-02"), skippedPinned)
			return nil
		}
		if !*yes {
			fmt.Printf("would delete %d chat(s) older than %s (pinned skipped: %d):\n", len(doomed), cutoff.Format("2006-01-02"), skippedPinned)
			for _, c := range doomed {
				fmt.Printf("  %s  %s  %s\n", c.ID, c.UpdatedAt.Format("2006-01-02"), c.Title)
			}
			fmt.Println("re-run with -yes to delete")
			return nil
		}
		n := 0
		for _, c := range doomed {
			if code, _, err := cc.Delete("/api/v1/chats/" + url.PathEscape(c.ID)); err == nil && code == 200 {
				n++
			}
		}
		fmt.Printf("pruned %d/%d chat(s) (pinned skipped: %d)\n", n, len(doomed), skippedPinned)
		return nil
	}
	if len(args) > 0 && args[0] == "delete-all" {
		dafs := flag.NewFlagSet("chats delete-all", flag.ExitOnError)
		yes := dafs.Bool("yes", false, "confirm deletion of every saved chat")
		dafs.Parse(reorderFlags(args[1:]))
		if !*yes {
			return fmt.Errorf("refusing to delete every saved chat without -yes")
		}
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
		var list []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no saved chats")
			return nil
		}
		n := 0
		for _, c := range list {
			if code, _, err := cc.Delete("/api/v1/chats/" + url.PathEscape(c.ID)); err == nil && code == 200 {
				n++
			}
		}
		fmt.Printf("deleted %d/%d saved chats\n", n, len(list))
		return nil
	}
	if len(args) > 0 && (args[0] == "pin" || args[0] == "unpin") {
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl chats %s <id>", args[0])
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Post("/api/v1/chats/"+url.PathEscape(args[1])+"/pin",
			map[string]any{"pinned": args[0] == "pin"})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat %s: %s", args[0], out)
		}
		word := "unpinned"
		if args[0] == "pin" {
			word = "pinned"
		}
		fmt.Printf("%s chat %s\n", word, args[1])
		return nil
	}
	if len(args) > 0 && args[0] == "delete" {
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl chats delete <id>")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Delete("/api/v1/chats/" + url.PathEscape(args[1]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat delete: %s", out)
		}
		fmt.Printf("deleted chat %s\n", args[1])
		return nil
	}
	fs := flag.NewFlagSet("chats", flag.ExitOnError)
	n := fs.Int("n", 20, "number of conversations")
	jsonOut := fs.Bool("json", false, "print the chat list as JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the chat list as CSV (F378)")
	// Accept "list" as an explicit subcommand: llmctl chats list [filter].
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	}
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
		if *csvOut { // keep the stream parseable (F378)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "title", "model", "messages", "updated_at"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no saved chats")
		return nil
	}
	if *csvOut { // F378
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "title", "model", "messages", "updated_at"})
		for _, c := range list {
			_ = w.Write([]string{c.ID, c.Title, c.Model, strconv.Itoa(c.Messages), c.UpdatedAt})
		}
		w.Flush()
		return w.Error()
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
		case "list", "show", "add", "export", "enable", "disable", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "add":
		// Create a skill from a JSON manifest file (F288).
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl skills add <manifest.json>")
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		var manifest map[string]any
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
		code, out, err := cc.Post("/api/v1/skills", manifest)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("skills add: %s", out)
		}
		var rec struct {
			Manifest struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"manifest"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("skill created: %s (%s)\n", rec.Manifest.Name, rec.Manifest.ID)
		return nil
	case "list":
		fs := flag.NewFlagSet("skills", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the skills as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the skill list as CSV (F384)")
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
		if *csvOut { // F384: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "enabled", "name", "description", "tags"})
			for _, s := range list {
				enabled := "off"
				if s.Manifest.Enabled {
					enabled = "on"
				}
				_ = w.Write([]string{s.Manifest.ID, enabled, s.Manifest.Name, s.Manifest.Description, strings.Join(s.Manifest.Tags, ";")})
			}
			w.Flush()
			return w.Error()
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
	case "export":
		// Dump a skill's manifest JSON (feed it to `skills add` elsewhere, F305).
		efs := flag.NewFlagSet("skills export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(rest))
		if efs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl skills export <id> [-out file]")
		}
		code, out, err := cc.Get("/api/v1/skills/" + url.PathEscape(efs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skill %s: %s", efs.Arg(0), out)
		}
		var rec struct {
			Manifest map[string]any `json:"manifest"`
		}
		if err := json.Unmarshal(out, &rec); err != nil || rec.Manifest == nil {
			return fmt.Errorf("skills export: unexpected response: %s", out)
		}
		data, err := json.MarshalIndent(rec.Manifest, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported skill %s to %s\n", efs.Arg(0), *outFile)
			return nil
		}
		fmt.Print(string(data))
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
		case "list", "show", "add", "update", "delete", "search", "export", "import":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "export":
		// Dump all memory entries as a JSON array (feed to `memory import`, F321).
		efs := flag.NewFlagSet("memory export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/memory")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory export: %s", out)
		}
		var list []struct {
			Entry struct {
				Content string   `json:"content"`
				Tags    []string `json:"tags,omitempty"`
				Source  string   `json:"source,omitempty"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		type memOut struct {
			Content string   `json:"content"`
			Tags    []string `json:"tags,omitempty"`
			Source  string   `json:"source,omitempty"`
		}
		recs := []memOut{}
		for _, r := range list {
			recs = append(recs, memOut{Content: r.Entry.Content, Tags: r.Entry.Tags, Source: r.Entry.Source})
		}
		data, err := json.MarshalIndent(recs, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %d memory entries to %s\n", len(recs), *outFile)
			return nil
		}
		fmt.Print(string(data))
		return nil
	case "import":
		// Bulk-create entries from a `memory export` file (F321).
		ifs := flag.NewFlagSet("memory import", flag.ExitOnError)
		ifs.Parse(reorderFlags(rest))
		if ifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory import <file.json>")
		}
		data, err := os.ReadFile(ifs.Arg(0))
		if err != nil {
			return err
		}
		var entries []struct {
			Content string   `json:"content"`
			Tags    []string `json:"tags,omitempty"`
			Source  string   `json:"source,omitempty"`
		}
		if err := json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		var created, skipped int
		for _, e := range entries {
			if strings.TrimSpace(e.Content) == "" {
				skipped++
				continue
			}
			code, out, err := cc.Post("/api/v1/memory", map[string]any{
				"content": e.Content, "tags": e.Tags, "source": e.Source,
			})
			if err != nil {
				return err
			}
			if code != 201 {
				fmt.Printf("  skip: %s\n", out)
				skipped++
				continue
			}
			created++
		}
		fmt.Printf("imported %d entries (%d skipped)\n", created, skipped)
		return nil
	case "update":
		ufs := flag.NewFlagSet("memory update", flag.ExitOnError)
		content := ufs.String("content", "", "new content")
		tags := ufs.String("tags", "", "comma-separated tags (replaces)")
		ufs.Parse(reorderFlags(rest))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory update <id> [-content C] [-tags a,b]")
		}
		if *content == "" && *tags == "" {
			return fmt.Errorf("nothing to change (pass -content and/or -tags)")
		}
		body := map[string]any{}
		if *content != "" {
			body["content"] = *content
		}
		if *tags != "" {
			list := []string{}
			for _, t := range strings.Split(*tags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					list = append(list, t)
				}
			}
			body["tags"] = list
		}
		code, out, err := cc.Do("PUT", "/api/v1/memory/"+url.PathEscape(ufs.Arg(0)), body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory update: %s", out)
		}
		fmt.Printf("memory entry %s updated\n", ufs.Arg(0))
		return nil
	case "search":
		sfs := flag.NewFlagSet("memory search", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw search JSON (for scripts)")
		sout := sfs.String("out", "", "write the raw search JSON to this file instead of stdout (F484)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory search <query> [-json] [-out FILE]")
		}
		q := strings.Join(sfs.Args(), " ")
		code, out, err := cc.Get("/api/v1/memory/search?q=" + url.QueryEscape(q))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory search: %s", out)
		}
		if *sout != "" { // F484: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*sout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *sout, len(out))
			return nil
		}
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		var res []struct {
			Entry struct {
				ID      string   `json:"id"`
				Content string   `json:"content"`
				Tags    []string `json:"tags"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		if len(res) == 0 {
			fmt.Printf("no memory entries match %q\n", q)
			return nil
		}
		for _, r := range res {
			fmt.Printf("%s  [%s]  %s\n", r.Entry.ID, strings.Join(r.Entry.Tags, ","), strings.ReplaceAll(strings.TrimSpace(r.Entry.Content), "\n", " "))
		}
		return nil
	case "list":
		fs := flag.NewFlagSet("memory", flag.ExitOnError)
		n := fs.Int("n", 30, "number of entries")
		jsonOut := fs.Bool("json", false, "print the entries as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the memory entries as CSV (F401)")
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
		if *csvOut { // F401: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "created_at", "tags", "content"})
			for _, m := range list {
				_ = w.Write([]string{m.Entry.ID, m.Entry.CreatedAt, strings.Join(m.Entry.Tags, ";"), m.Entry.Content})
			}
			w.Flush()
			return w.Error()
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
		case "list", "show", "create", "update", "docs", "doc", "add-doc", "export", "search", "embed", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "embed":
		efs := flag.NewFlagSet("kb embed", flag.ExitOnError)
		urlF := efs.String("url", "", "embeddings endpoint (base like http://host:port/v1 or full /embeddings URL)")
		modelF := efs.String("model", "", "embedding model name")
		keyF := efs.String("key", "", "bearer key for the endpoint")
		clearF := efs.Bool("clear", false, "remove the embedding config (fall back to TF-IDF)")
		efs.Parse(reorderFlags(rest))
		if efs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb embed <kb-id> [-url U -model M [-key K]] | -clear")
		}
		kbID := efs.Arg(0)
		body := map[string]any{}
		if *clearF {
			body = map[string]any{"url": "", "model": ""}
		} else {
			if *urlF == "" || *modelF == "" {
				return fmt.Errorf("kb embed needs -url and -model (or -clear)")
			}
			body["url"] = *urlF
			body["model"] = *modelF
			if *keyF != "" {
				body["apiKey"] = *keyF
			}
		}
		code, out, err := cc.Do("PUT", "/api/v1/knowledge-bases/"+url.PathEscape(kbID)+"/embed", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb embed: %s", out)
		}
		if *clearF {
			fmt.Printf("knowledge base %s embedding config cleared (TF-IDF search)\n", kbID)
			return nil
		}
		fmt.Printf("knowledge base %s now uses embeddings (%s)\n", kbID, *modelF)
		return nil
	case "search":
		sfs := flag.NewFlagSet("kb search", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw search JSON (for scripts)")
		limit := sfs.Int("limit", 10, "max results")
		sout := sfs.String("out", "", "write the raw search JSON to this file instead of stdout (F483)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl kb search <kb-id> <query> [-limit N] [-json] [-out FILE]")
		}
		kbID := sfs.Arg(0)
		query := strings.Join(sfs.Args()[1:], " ")
		code, out, err := cc.Post("/api/v1/knowledge-bases/"+url.PathEscape(kbID)+"/search",
			map[string]any{"q": query, "limit": *limit})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb search: %s", out)
		}
		if *sout != "" { // F483: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*sout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *sout, len(out))
			return nil
		}
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		var res []struct {
			DocID      string  `json:"doc_id"`
			Filename   string  `json:"filename"`
			ChunkText  string  `json:"chunk_text"`
			Score      float64 `json:"score"`
			ChunkIndex int     `json:"chunk_index"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		if len(res) == 0 {
			fmt.Printf("no chunks match %q in %s\n", query, kbID)
			return nil
		}
		for _, r := range res {
			sn := strings.TrimSpace(r.ChunkText)
			sn = strings.ReplaceAll(sn, "\n", " ")
			if len(sn) > 100 {
				sn = sn[:100] + "…"
			}
			fmt.Printf("%-24s chunk %-4d %6.3f  %s\n", r.Filename, r.ChunkIndex, r.Score, sn)
		}
		return nil
	case "list":
		fs := flag.NewFlagSet("kb", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the knowledge bases as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the knowledge-base list as CSV (F379)")
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
			if *csvOut { // keep the stream parseable (F379)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "name", "docs", "description"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no knowledge bases")
			return nil
		}
		if *csvOut { // F379
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "docs", "description"})
			for _, kb := range list {
				_ = w.Write([]string{kb.KB.ID, kb.KB.Name, strconv.Itoa(kb.KB.DocCount), kb.KB.Description})
			}
			w.Flush()
			return w.Error()
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
		sfs := flag.NewFlagSet("kb show", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw KB JSON (for scripts)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb show <id> [-json]")
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb %s: %s", sfs.Arg(0), out)
		}
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		// Human view (F366): header, search mode, then the document list.
		var rec struct {
			ID  string `json:"id"`
			Dir string `json:"dir"`
			KB  struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				DocCount    int    `json:"doc_count"`
				CreatedAt   string `json:"created_at"`
				UpdatedAt   string `json:"updated_at"`
				Embed       *struct {
					URL   string `json:"url"`
					Model string `json:"model"`
				} `json:"embed"`
			} `json:"kb"`
			Docs []struct {
				Filename    string `json:"filename"`
				ContentType string `json:"content_type"`
				Size        int64  `json:"size"`
				Source      string `json:"source"`
				CreatedAt   string `json:"created_at"`
			} `json:"docs"`
		}
		if err := json.Unmarshal(out, &rec); err != nil {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("%s  %s\n", rec.ID, rec.KB.Name)
		if rec.KB.Description != "" {
			fmt.Printf("  description: %s\n", rec.KB.Description)
		}
		search := "TF-IDF (no embedding model set)"
		if rec.KB.Embed != nil {
			search = fmt.Sprintf("embeddings: %s @ %s", rec.KB.Embed.Model, rec.KB.Embed.URL)
		}
		fmt.Printf("  docs:        %d\n", rec.KB.DocCount)
		fmt.Printf("  search:      %s\n", search)
		fmt.Printf("  created:     %s   updated: %s\n", shortTime(rec.KB.CreatedAt), shortTime(rec.KB.UpdatedAt))
		if len(rec.Docs) > 0 {
			fmt.Printf("  documents:\n")
			for _, d := range rec.Docs {
				src := ""
				if d.Source != "" {
					src = "  " + d.Source
				}
				fmt.Printf("    %-40s %8s  %-16s %s%s\n", d.Filename, fmtSize(d.Size), d.ContentType, shortTime(d.CreatedAt), src)
			}
		}
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
	case "doc":
		// Download one document's stored text (GET …/documents/{id}/content, F320).
		dfs := flag.NewFlagSet("kb doc", flag.ExitOnError)
		outFile := dfs.String("out", "", "write to this file instead of stdout")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl kb doc <kb-id> <doc-id> [-out FILE]")
		}
		code, body, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(dfs.Arg(0)) +
			"/documents/" + url.PathEscape(dfs.Arg(1)) + "/content")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb doc: %s", body)
		}
		if *outFile != "" {
			if err := os.WriteFile(*outFile, body, 0o644); err != nil {
				return err
			}
			fmt.Printf("saved %s to %s (%s)\n", dfs.Arg(1), *outFile, fmtSize(int64(len(body))))
			return nil
		}
		fmt.Print(string(body))
		return nil
	case "export":
		// Write every document of a KB into a directory (re-import with
		// repeated `kb add-doc`, F324).
		xfs := flag.NewFlagSet("kb export", flag.ExitOnError)
		outDir := xfs.String("out", "", "target directory (default ./kb-<id>)")
		xfs.Parse(reorderFlags(rest))
		if xfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb export <kb-id> [-out DIR]")
		}
		kbID := xfs.Arg(0)
		dir := *outDir
		if dir == "" {
			dir = "kb-" + kbID
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(kbID) + "/documents")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb export: %s", out)
		}
		var docs []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		}
		if err := json.Unmarshal(out, &docs); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var n int
		for _, d := range docs {
			code, body, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(kbID) +
				"/documents/" + url.PathEscape(d.ID) + "/content")
			if err != nil {
				return err
			}
			if code != 200 {
				fmt.Printf("  skip %s: %s\n", d.Filename, body)
				continue
			}
			name := filepath.Base(strings.ReplaceAll(d.Filename, "\\", "/")) // no path escape
			if name == "." || name == "/" || name == "" {
				name = d.ID
			}
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
				return err
			}
			n++
		}
		fmt.Printf("exported %d document(s) from %s to %s/\n", n, kbID, dir)
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
	case "update":
		// Read-modify-write: PUT replaces name+description wholesale, so
		// fetch the current values and apply only the flags given (F291).
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl kb update <id> [-name N] [-desc D]")
		}
		ufs := flag.NewFlagSet("kb update", flag.ExitOnError)
		name := ufs.String("name", "", "new name")
		desc := ufs.String("desc", "", "new description")
		ufs.Parse(reorderFlags(rest[1:]))
		if *name == "" && *desc == "" {
			return fmt.Errorf("nothing to change (pass -name and/or -desc)")
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb update: %s", out)
		}
		var cur struct {
			KB struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"kb"`
		}
		if err := json.Unmarshal(out, &cur); err != nil {
			return fmt.Errorf("kb update: unexpected record: %s", out)
		}
		newName, newDesc := cur.KB.Name, cur.KB.Description
		if *name != "" {
			newName = *name
		}
		if *desc != "" {
			newDesc = *desc
		}
		code, out, err = cc.Do("PUT", "/api/v1/knowledge-bases/"+url.PathEscape(rest[0]),
			map[string]any{"name": newName, "description": newDesc})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb update: %s", out)
		}
		fmt.Printf("knowledge base %s updated\n", rest[0])
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
		csvOut := fs.Bool("csv", false, "output the tool list as CSV (F389)")
		outFile := fs.String("out", "", "write the raw tools JSON to this file instead of stdout (F486)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/tools")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("tools returned %d: %s", code, out)
		}
		if *outFile != "" { // F486: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
		if *csvOut { // F389: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "enabled", "sandbox", "runtime", "name", "description"})
			for _, t := range list {
				rt := t.Manifest.Runtime
				if rt == "" {
					rt = "python"
				}
				_ = w.Write([]string{t.ID, strconv.FormatBool(t.Enabled), strconv.FormatBool(t.Sandbox),
					rt, t.Manifest.Name, t.Manifest.Description})
			}
			w.Flush()
			return w.Error()
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
		argsJSON := rfs.String("args", "{}", "JSON object of tool arguments (use - to read from stdin, F338)")
		argsFile := rfs.String("args-file", "", "read the JSON arguments from this file (F338)")
		timeout := rfs.Int("timeout", 0, "timeout in seconds (0 = default)")
		outFile := rfs.String("out", "", "write the tool result to this file instead of stdout (F458)")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl tools run <id> [-args JSON|-|-args-file F] [-timeout N] [-out FILE]")
		}
		rawArgs := *argsJSON
		if *argsFile != "" {
			b, err := os.ReadFile(*argsFile)
			if err != nil {
				return err
			}
			rawArgs = string(b)
		} else if rawArgs == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			rawArgs = string(b)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(rawArgs), &parsed); err != nil {
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
		if *outFile != "" { // F458: result only, for scripts
			if err := os.WriteFile(*outFile, []byte(res.Result), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(res.Result))
		} else {
			fmt.Print(res.Result)
		}
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
		case "list", "add", "remove", "enable", "disable", "test", "export", "import":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "export":
		// Dump all MCP server configs as a JSON array (feed to `mcp import`,
		// F330). NOTE: the file may contain env values that hold secrets.
		efs := flag.NewFlagSet("mcp export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/mcp")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("mcp export: %s", out)
		}
		var list []map[string]any
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %d MCP servers to %s (may contain secrets in env)\n", len(list), *outFile)
			return nil
		}
		fmt.Print(string(data))
		return nil
	case "import":
		// Recreate MCP servers from an `mcp export` file (F330). Fresh ids;
		// the create endpoint forces enabled, so a disabled source is
		// re-disabled afterwards.
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl mcp import <file.json>")
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		var list []map[string]any
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		var created, skipped int
		for _, s := range list {
			delete(s, "id") // the store mints its own ids
			wantEnabled := true
			if v, ok := s["enabled"].(bool); ok {
				wantEnabled = v
			}
			delete(s, "enabled") // create forces enabled=true
			code, out, err := cc.Post("/api/v1/mcp", s)
			if err != nil {
				return err
			}
			if code != 201 && code != 200 {
				fmt.Printf("  skip %v: %s\n", s["name"], out)
				skipped++
				continue
			}
			var rec struct {
				ID string `json:"id"`
			}
			json.Unmarshal(out, &rec)
			if !wantEnabled && rec.ID != "" {
				cc.Do("POST", "/api/v1/mcp/"+url.PathEscape(rec.ID)+"/enabled", map[string]any{"enabled": false})
			}
			created++
		}
		fmt.Printf("imported %d MCP servers (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("mcp", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the servers as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the MCP server list as CSV (F403)")
		outFile := fs.String("out", "", "write the raw MCP servers JSON to this file instead of stdout (F487)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/mcp")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("mcp returned %d: %s", code, out)
		}
		if *outFile != "" { // F487: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
		if *csvOut { // F403: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "enabled", "command"})
			for _, s := range list {
				cmd := s.Command
				if len(s.Args) > 0 {
					cmd += " " + strings.Join(s.Args, " ")
				}
				_ = w.Write([]string{s.ID, s.Name, strconv.FormatBool(s.Enabled), cmd})
			}
			w.Flush()
			return w.Error()
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
			return fmt.Errorf("usage: llmctl mcp test <id> [--csv|--json] [-out FILE]")
		}
		tfs := flag.NewFlagSet("mcp test", flag.ExitOnError)
		jsonOut := tfs.Bool("json", false, "print the advertised tools as JSON (for scripts)")
		csvOut := tfs.Bool("csv", false, "output the advertised tools as CSV (F436)")
		outFile := tfs.String("out", "", "write the advertised tools JSON to this file instead of stdout (implies -json, F459)")
		tfs.Parse(reorderFlags(rest[1:]))
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
		if *jsonOut || *outFile != "" { // F436; -out implies the JSON shape (F459)
			b, err := json.Marshal(res.Tools)
			if err != nil {
				return err
			}
			if *outFile != "" {
				if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
				return nil
			}
			fmt.Println(string(b))
			return nil
		}
		if *csvOut { // F436: header-only when the server advertises no tools
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"name", "description"})
			for _, t := range res.Tools {
				_ = w.Write([]string{t.Name, t.Description})
			}
			w.Flush()
			return w.Error()
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
	modelF := fs.String("model", "", "show only this model id (F349)")
	keyF := fs.String("key", "", "with -keys: show only this key id (F349)")
	csvOut := fs.Bool("csv", false, "output the cost table as CSV (F370)")
	jsonOut := fs.Bool("json", false, "print the raw cost JSON (for scripts)")
	outFile := fs.String("out", "", "write the cost JSON to this file instead of stdout (F470)")
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
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F470)
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
		if *outFile != "" {
			if err := os.WriteFile(*outFile, append(b, '\n'), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b)+1)
		} else {
			fmt.Println(string(b))
		}
		return nil
	}
	if !*csvOut {
		fmt.Printf("total: $%.4f — %d tokens (in %d / out %d)\n\n",
			totals.CostUSD, totals.TotalTokens, totals.InputTokens, totals.OutputTokens)
	}
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
		if *modelF != "" && !strings.EqualFold(id, *modelF) {
			continue
		}
		if *byKey && *keyF != "" && !strings.EqualFold(id, *keyF) {
			continue
		}
		r := row{id: id}
		r.u = u
		rows = append(rows, r)
	}
	if len(rows) == 0 && (*modelF != "" || (*byKey && *keyF != "")) {
		fmt.Println("no cost recorded for that id")
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].u.Cost > rows[j].u.Cost })
	label := "MODEL"
	if *byKey {
		label = "KEY"
	}
	if *csvOut { // F370
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{strings.ToLower(label), "name", "requests", "input_tokens", "output_tokens", "total_tokens", "cost_usd"})
		for _, r := range rows {
			_ = w.Write([]string{r.id, names[r.id], strconv.FormatInt(r.u.Requests, 10),
				strconv.FormatInt(r.u.InputTok, 10), strconv.FormatInt(r.u.OutputTok, 10),
				strconv.FormatInt(r.u.TotalTok, 10), strconv.FormatFloat(r.u.Cost, 'f', 4, 64)})
		}
		w.Flush()
		return w.Error()
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
	if len(args) > 0 && args[0] == "add" {
		// Create a custom prompt template (F306).
		afs := flag.NewFlagSet("prompts add", flag.ExitOnError)
		name := afs.String("name", "", "display name (required)")
		category := afs.String("category", "", "category (optional)")
		desc := afs.String("desc", "", "description (optional)")
		content := afs.String("content", "", "template text (or use -file / piped stdin)")
		file := afs.String("file", "", "read the template text from this file")
		afs.Parse(reorderFlags(args[1:]))
		if afs.NArg() == 0 || *name == "" {
			return fmt.Errorf("usage: llmctl prompts add <id> -name N [-category C] [-desc D] (-content TEXT | -file F | piped stdin)")
		}
		text := *content
		if text == "" && *file != "" {
			b, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			text = string(b)
		}
		if text == "" {
			if fi, _ := os.Stdin.Stat(); fi.Mode()&os.ModeCharDevice == 0 {
				if b, err := io.ReadAll(os.Stdin); err == nil {
					text = string(b)
				}
			}
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("template text is required (-content, -file, or piped stdin)")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Post("/api/v1/prompts", map[string]any{
			"id": afs.Arg(0), "name": *name, "category": *category,
			"description": *desc, "content": text,
		})
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("prompts add: %s", out)
		}
		fmt.Printf("prompt created: %s (%s)\n", *name, afs.Arg(0))
		return nil
	}
	if len(args) > 0 && args[0] == "update" {
		// Edit a custom prompt template (F308); only the flags given change.
		ufs := flag.NewFlagSet("prompts update", flag.ExitOnError)
		name := ufs.String("name", "", "new display name")
		category := ufs.String("category", "", "new category")
		desc := ufs.String("desc", "", "new description")
		content := ufs.String("content", "", "new template text (or -file / piped stdin)")
		file := ufs.String("file", "", "read the new template text from this file")
		ufs.Parse(reorderFlags(args[1:]))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl prompts update <id> [-name N] [-category C] [-desc D] [-content T | -file F | piped stdin]")
		}
		body := map[string]any{}
		if *name != "" {
			body["name"] = *name
		}
		if *category != "" {
			body["category"] = *category
		}
		if *desc != "" {
			body["description"] = *desc
		}
		text := *content
		if text == "" && *file != "" {
			b, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			text = string(b)
		}
		if text == "" {
			if fi, _ := os.Stdin.Stat(); fi.Mode()&os.ModeCharDevice == 0 {
				if b, err := io.ReadAll(os.Stdin); err == nil {
					text = string(b)
				}
			}
		}
		if text != "" {
			body["content"] = text
		}
		if len(body) == 0 {
			return fmt.Errorf("nothing to change (pass -name, -category, -desc or -content)")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Put("/api/v1/prompts/"+url.PathEscape(ufs.Arg(0)), body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompts update: %s", out)
		}
		fmt.Printf("updated prompt %s\n", ufs.Arg(0))
		return nil
	}
	if len(args) > 0 && args[0] == "delete" {
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl prompts delete <id>")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Delete("/api/v1/prompts/" + url.PathEscape(args[1]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompts delete: %s", out)
		}
		fmt.Printf("deleted prompt %s\n", args[1])
		return nil
	}
	if len(args) > 0 && args[0] == "export" {
		// Dump custom templates as a JSON array (feed to `prompts import`, F323).
		efs := flag.NewFlagSet("prompts export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(args[1:]))
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/prompts")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompts export: %s", out)
		}
		var all []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Content     string `json:"content"`
			Custom      bool   `json:"custom"`
		}
		if err := json.Unmarshal(out, &all); err != nil {
			return err
		}
		type pOut struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			Category    string `json:"category,omitempty"`
			Content     string `json:"content"`
		}
		recs := []pOut{}
		for _, p := range all {
			if p.Custom {
				recs = append(recs, pOut{ID: p.ID, Name: p.Name, Description: p.Description, Category: p.Category, Content: p.Content})
			}
		}
		data, err := json.MarshalIndent(recs, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %d custom prompts to %s\n", len(recs), *outFile)
			return nil
		}
		fmt.Print(string(data))
		return nil
	}
	if len(args) > 0 && args[0] == "import" {
		// Bulk-create templates from a `prompts export` file (F323).
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl prompts import <file.json>")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		var entries []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Content     string `json:"content"`
		}
		if err := json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		var created, skipped int
		for _, e := range entries {
			code, out, err := cc.Post("/api/v1/prompts", map[string]any{
				"id": e.ID, "name": e.Name, "description": e.Description,
				"category": e.Category, "content": e.Content,
			})
			if err != nil {
				return err
			}
			if code != 201 {
				fmt.Printf("  skip %s: %s\n", e.ID, out)
				skipped++
				continue
			}
			created++
		}
		fmt.Printf("imported %d prompts (%d skipped)\n", created, skipped)
		return nil
	}
	if len(args) > 0 && args[0] == "categories" {
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/prompts/categories")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("categories returned %d: %s", code, out)
		}
		var cats []string
		if err := json.Unmarshal(out, &cats); err != nil {
			fmt.Print(string(out))
			return nil
		}
		for _, c := range cats {
			fmt.Println(c)
		}
		return nil
	}
	if len(args) > 0 && args[0] == "render" {
		rfs := flag.NewFlagSet("prompts render", flag.ExitOnError)
		input := rfs.String("input", "", "text to substitute for {{input}} (default: piped stdin)")
		outFile := rfs.String("out", "", "write the rendered prompt to this file instead of stdout (F442)")
		var vlist stringList
		rfs.Var(&vlist, "var", "substitute {{k}} for v (repeatable, F347)")
		rfs.Parse(reorderFlags(args[1:]))
		if rfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl prompts render <id> [-input TEXT | piped stdin] [-var k=v ...] [-out file]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/prompts/" + url.PathEscape(rfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompt %q not found (%d)", rfs.Arg(0), code)
		}
		var p struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(out, &p); err != nil {
			return err
		}
		text := *input
		if text == "" {
			if b, err := io.ReadAll(os.Stdin); err == nil {
				text = strings.TrimSpace(string(b))
			}
		}
		out2 := strings.ReplaceAll(p.Content, "{{input}}", text)
		// Extra placeholders: -var k=v replaces {{k}} (F347).
		for _, kv := range vlist {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return fmt.Errorf("-var expects k=v, got %q", kv)
			}
			out2 = strings.ReplaceAll(out2, "{{"+strings.TrimSpace(k)+"}}", v)
		}
		if *outFile != "" { // F442: rendered text only, for scripts
			if err := os.WriteFile(*outFile, []byte(out2+"\n"), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s (%d bytes)\n", *outFile, len(out2)+1)
			return nil
		}
		fmt.Println(out2)
		return nil
	}
	if len(args) > 0 && args[0] == "show" {
		sfs := flag.NewFlagSet("prompts show", flag.ExitOnError)
		outFile := sfs.String("out", "", "write the prompt content to this file instead of stdout (F438)")
		sfs.Parse(reorderFlags(args[1:]))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl prompts show <id> [-out file]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/prompts/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompt %q not found (%d)", sfs.Arg(0), code)
		}
		var p struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(out, &p); err != nil {
			return err
		}
		if *outFile != "" { // F438: raw content only, for scripts
			if err := os.WriteFile(*outFile, []byte(p.Content+"\n"), 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s (%d bytes)\n", *outFile, len(p.Content)+1)
			return nil
		}
		fmt.Printf("%s (%s)\n\n%s\n", p.Name, p.ID, p.Content)
		return nil
	}
	// Accept "list" as an explicit subcommand (F382), like chats does.
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("prompts", flag.ExitOnError)
	category := fs.String("category", "", "only this category")
	csvOut := fs.Bool("csv", false, "output the prompt list as CSV (F382)")
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
		if *csvOut { // keep the stream parseable (F382)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "category", "name", "description"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no prompts match")
		return nil
	}
	if *csvOut { // F382
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "category", "name", "description"})
		for _, p := range shown {
			_ = w.Write([]string{p.ID, p.Category, p.Name, p.Description})
		}
		w.Flush()
		return w.Error()
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
		csvOut := fs.Bool("csv", false, "output the file list as CSV (F381)")
		outFile := fs.String("out", "", "write the raw files JSON to this file instead of stdout (F485)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/files")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("files returned %d: %s", code, out)
		}
		if *outFile != "" { // F485: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
			if *csvOut { // keep the stream parseable (F381)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "filename", "content_type", "size_bytes", "source", "created_at"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no uploaded files (upload via the web UI)")
			return nil
		}
		if *csvOut { // F381
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "filename", "content_type", "size_bytes", "source", "created_at"})
			for _, f := range list {
				_ = w.Write([]string{f.ID, f.Entry.Filename, f.Entry.ContentType,
					strconv.FormatInt(f.Entry.Size, 10), f.Entry.Source,
					f.Entry.CreatedAt.UTC().Format(time.RFC3339)})
			}
			w.Flush()
			return w.Error()
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
	outFile := fs.String("out", "", "write the raw settings JSON to this file instead of stdout (F471)")
	fs.Parse(reorderFlags(args))
	code, out, err := cc.Get("/api/v1/settings")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("settings returned %d: %s", code, out)
	}
	if *outFile != "" { // F471: raw JSON to a file, stdout stays clean
		if err := os.WriteFile(*outFile, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		return nil
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
	temperature := fs.Float64("temperature", -1, "sampling temperature (-1 = model default, F343)")
	concurrency := fs.Int("concurrency", 4, "parallel requests (max 8)")
	key := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (needed when auth is on; default $LLMCTL_API_KEY)")
	file := fs.String("file", "", "read prompts from this file, one per line")
	jsonOut := fs.Bool("json", false, "print the raw batch JSON (for scripts)")
	outFile := fs.String("out", "", "write the raw batch JSON to this file instead of stdout (implies -json, F449)")
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
		return fmt.Errorf("usage: llmctl batch -model ID [-system S] [-key K] [-temperature T] [-file F|piped stdin|prompts…]")
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
	if *temperature >= 0 {
		body["temperature"] = *temperature
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
	if *outFile != "" { // F449: raw JSON to a file, stdout stays clean
		if err := os.WriteFile(*outFile, out, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
		return nil
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
		csvOut := fs.Bool("csv", false, "output the key table as CSV (F374)")
		outFile := fs.String("out", "", "write the raw keys JSON to this file instead of stdout (F481)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/keys")
		if err != nil {
			return err
		}
		if *outFile != "" { // F481: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
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
			if *csvOut { // keep the stream parseable (F374)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "name", "prefix", "disabled", "expires_at", "last_used", "used_tokens", "token_quota", "used_requests", "max_requests", "used_cost_usd", "today_cost_usd", "daily_cost_budget_usd", "note"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no keys")
			return nil
		}
		if *csvOut { // F374
			rfc := func(t *time.Time) string {
				if t == nil {
					return ""
				}
				return t.UTC().Format(time.RFC3339)
			}
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "prefix", "disabled", "expires_at", "last_used", "used_tokens", "token_quota", "used_requests", "max_requests", "used_cost_usd", "today_cost_usd", "daily_cost_budget_usd", "note"})
			for _, k := range list {
				_ = w.Write([]string{k.ID, k.Name, k.Prefix, strconv.FormatBool(k.Disabled),
					rfc(k.ExpiresAt), rfc(k.LastUsed),
					strconv.FormatInt(k.UsedTok, 10), strconv.FormatInt(k.TokenQuota, 10),
					strconv.FormatInt(k.UsedReqs, 10), strconv.FormatInt(k.MaxRequests, 10),
					strconv.FormatFloat(k.UsedCost, 'f', 4, 64), strconv.FormatFloat(k.TodayCost, 'f', 4, 64),
					strconv.FormatFloat(k.DailyCostBudgetUSD, 'f', 4, 64), k.Note})
			}
			w.Flush()
			return w.Error()
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
		return fmt.Errorf("usage: llmctl autostart <list|add|remove|export|import>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "export":
		// Dump all auto-start entries as a JSON array (feed to `autostart import`, F329).
		efs := flag.NewFlagSet("autostart export", flag.ExitOnError)
		outFile := efs.String("out", "", "write to this file instead of stdout")
		efs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/autostart")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("autostart export: %s", out)
		}
		var list []map[string]any
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("exported %d auto-start entries to %s\n", len(list), *outFile)
			return nil
		}
		fmt.Print(string(data))
		return nil
	case "import":
		// Re-create auto-start entries from an `autostart export` file (F329);
		// same-model entries are replaced (the POST upserts).
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl autostart import <file.json>")
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		var list []map[string]any
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("import file: %w", err)
		}
		var created, skipped int
		for _, s := range list {
			code, out, err := cc.Post("/api/v1/autostart", s)
			if err != nil {
				return err
			}
			if code/100 != 2 {
				fmt.Printf("  skip %v: %s\n", s["modelId"], out)
				skipped++
				continue
			}
			created++
		}
		fmt.Printf("imported %d auto-start entries (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("autostart list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw autostart JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the autostart list as CSV (F390)")
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
		if *csvOut { // F390: header-only when empty keeps the stream parseable
			csvStr := func(v any) string {
				if v == nil {
					return ""
				}
				return fmt.Sprint(v)
			}
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"name", "model", "backend", "port", "vars"})
			for _, s := range list {
				vars := ""
				if m, ok := s["vars"].(map[string]any); ok {
					sm := make(map[string]string, len(m))
					for k, v := range m {
						sm[k] = fmt.Sprint(v)
					}
					vars = sortedVarsJoin(sm, ";")
				}
				_ = w.Write([]string{csvStr(s["name"]), csvStr(s["modelId"]),
					csvStr(s["backendId"]), csvStr(s["port"]), vars})
			}
			w.Flush()
			return w.Error()
		}
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

// kbContextFor searches a knowledge base for the query and builds the
// context block shared by the one-shot -kb flag and the REPL /kb command (F342).
// Returns "" when nothing matches.
func kbContextFor(cc *daemon.ControlClient, kbID, query string) (string, error) {
	code, out, err := cc.Post("/api/v1/knowledge-bases/"+url.PathEscape(kbID)+"/search",
		map[string]any{"q": query, "limit": 3})
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("kb search: %s", out)
	}
	var hits []struct {
		Filename  string  `json:"filename"`
		Score     float64 `json:"score"`
		ChunkText string  `json:"chunk_text"`
	}
	if err := json.Unmarshal(out, &hits); err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Relevant context from knowledge base %q:\n\n", kbID)
	for i, h := range hits {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&sb, "Document: %s (relevance: %.1f%%)\n%s", h.Filename, h.Score*100, h.ChunkText)
	}
	return sb.String(), nil
}

func cmdChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	modelFlag := fs.String("model", "", "model id to chat with")
	keyFlag := fs.String("key", os.Getenv("LLMCTL_API_KEY"), "API key (if auth is enabled; default $LLMCTL_API_KEY)")
	system := fs.String("system", "", "system prompt prepended to the conversation (F188)")
	prompt := fs.String("prompt", "", "one-shot mode: send this text (or pipe stdin) and print the reply, no REPL")
	agentMode := fs.Bool("agent", false, "with -prompt: run the tool-using agent loop instead of a plain chat (F285)")
	saveChat := fs.String("save", "", "with -prompt: save the exchange as a saved chat under this title (F294)")
	contChat := fs.String("chat", "", "with -prompt: continue this saved chat — its history is sent and the reply is appended to it (F297)")
	contLast := fs.Bool("continue", false, "with -prompt: continue the most recently updated saved chat (F312)")
	kbFlag := fs.String("kb", "", "with -prompt: prepend the top knowledge-base matches for the prompt as context (like the Chat UI)")
	jsonOut := fs.Bool("json", false, "with -prompt: print {model, prompt, reply, chat_id} as one JSON object instead of plain text (F313)")
	temp := fs.Float64("temperature", -1, "with -prompt: sampling temperature (-1 = model default, F319)")
	maxTok := fs.Int("max-tokens", 0, "with -prompt: max tokens for the reply (0 = model default, F319)")
	outFile := fs.String("out", "", "with -prompt: write the reply text to this file instead of stdout (F448)")
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
	if *agentMode {
		text := *prompt
		if text == "" {
			if fi, _ := os.Stdin.Stat(); fi.Mode()&os.ModeCharDevice == 0 {
				if b, err := io.ReadAll(os.Stdin); err == nil {
					text = strings.TrimSpace(string(b))
				}
			}
		}
		if text == "" {
			return fmt.Errorf("-agent: no prompt (use -prompt or pipe text to stdin)")
		}
		body, _ := json.Marshal(map[string]any{
			"model":    model,
			"messages": []map[string]string{{"role": "user", "content": text}},
		})
		req, err := http.NewRequest(http.MethodPost, cc.Base+"/api/v1/chat/agent", strings.NewReader(string(body)))
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
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("agent chat returned %d: %s", resp.StatusCode, string(b))
		}
		rd := bufio.NewReader(resp.Body)
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				break
			}
			l = strings.TrimSpace(l)
			if !strings.HasPrefix(l, "data:") {
				continue
			}
			var ev struct {
				Type    string `json:"type"`
				Content string `json:"content"`
				Name    string `json:"name"`
				Query   string `json:"query"`
				Error   string `json:"error"`
				Steps   int    `json:"steps"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(l[5:])), &ev) != nil {
				continue
			}
			switch ev.Type {
			case "delta":
				fmt.Print(ev.Content)
			case "tool":
				fmt.Printf("\n[tool %s: %s]\n", ev.Name, ev.Query)
			case "error":
				fmt.Fprintln(os.Stderr, "\nerror:", ev.Error)
				return fmt.Errorf("agent failed")
			case "done":
				fmt.Printf("\n[done in %d step(s)]\n", ev.Steps)
				return nil
			}
		}
		return nil
	}
	oneShot := *prompt
	if oneShot == "" {
		if fi, _ := os.Stdin.Stat(); fi.Mode()&os.ModeCharDevice == 0 {
			// piped stdin in one-shot mode: the prompt is stdin itself
			if b, err := io.ReadAll(os.Stdin); err == nil {
				oneShot = strings.TrimSpace(string(b))
			}
		}
	}
	if oneShot != "" {
		hist := []map[string]any{}
		var contMsgs []map[string]string
		var contTitle string
		if *contLast && *contChat == "" {
			// Resolve the most recently updated chat (F312). The list is
			// pinned-first, so scan for the newest updatedAt ourselves.
			code, out, err := cc.Get("/api/v1/chats")
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("chats list: %s", out)
			}
			var chats []struct {
				ID        string    `json:"id"`
				UpdatedAt time.Time `json:"updatedAt"`
			}
			if err := json.Unmarshal(out, &chats); err != nil {
				return err
			}
			var best time.Time
			for _, c := range chats {
				if c.UpdatedAt.After(best) {
					best, *contChat = c.UpdatedAt, c.ID
				}
			}
			if *contChat == "" {
				return fmt.Errorf("no saved chats to continue")
			}
			fmt.Fprintf(os.Stderr, "continuing chat %s\n", *contChat)
		}
		if *contChat != "" {
			code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(*contChat))
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("chat %s: %s", *contChat, out)
			}
			var ch struct {
				Title    string `json:"title"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(out, &ch); err != nil {
				return err
			}
			contTitle = ch.Title
			for _, m := range ch.Messages {
				hist = append(hist, map[string]any{"role": m.Role, "content": m.Content})
				contMsgs = append(contMsgs, map[string]string{"role": m.Role, "content": m.Content})
			}
		}
		if strings.TrimSpace(*system) != "" && len(contMsgs) == 0 {
			hist = append(hist, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
		}
		if *kbFlag != "" {
			ctxMsg, err := kbContextFor(cc, *kbFlag, oneShot)
			if err != nil {
				return err
			}
			if ctxMsg != "" {
				hist = append(hist, map[string]any{"role": "user", "content": ctxMsg})
			}
		}
		hist = append(hist, map[string]any{"role": "user", "content": oneShot})
		reqBody := map[string]any{"model": model, "messages": hist, "stream": true}
		if *temp >= 0 {
			reqBody["temperature"] = *temp
		}
		if *maxTok > 0 {
			reqBody["max_tokens"] = *maxTok
		}
		b, _ := json.Marshal(reqBody)
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
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("chat returned %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var reply strings.Builder
		rd := bufio.NewReader(resp.Body)
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				break
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
			if json.Unmarshal([]byte(data), &chunk) == nil && len(chunk.Choices) > 0 {
				if !*jsonOut && *outFile == "" {
					fmt.Print(chunk.Choices[0].Delta.Content)
				}
				reply.WriteString(chunk.Choices[0].Delta.Content)
			}
		}
		if !*jsonOut && *outFile == "" {
			fmt.Println()
		}
		if *outFile != "" { // F448: reply text only, for scripts
			if err := os.WriteFile(*outFile, []byte(reply.String()+"\n"), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, reply.Len()+1)
		}
		savedID := *contChat
		if *contChat != "" {
			msgs := append(contMsgs,
				map[string]string{"role": "user", "content": oneShot},
				map[string]string{"role": "assistant", "content": reply.String()})
			code, out, err := cc.Put("/api/v1/chats/"+url.PathEscape(*contChat), map[string]any{
				"title":    contTitle,
				"model":    model,
				"messages": msgs,
			})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("chat update: %s", out)
			}
			if !*jsonOut {
				fmt.Printf("[appended to chat %s]\n", *contChat)
			}
		} else if *saveChat != "" {
			msgs := []map[string]string{}
			for _, h := range hist {
				msgs = append(msgs, map[string]string{"role": fmt.Sprint(h["role"]), "content": fmt.Sprint(h["content"])})
			}
			msgs = append(msgs, map[string]string{"role": "assistant", "content": reply.String()})
			code, out, err := cc.Post("/api/v1/chats", map[string]any{
				"title":    *saveChat,
				"model":    model,
				"messages": msgs,
			})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("chat save: %s", out)
			}
			var saved struct {
				ID string `json:"id"`
			}
			json.Unmarshal(out, &saved)
			savedID = saved.ID
			if !*jsonOut {
				fmt.Printf("[saved as chat %s: %q]\n", saved.ID, *saveChat)
			}
		}
		if *jsonOut {
			b, _ := json.Marshal(map[string]any{
				"model": model, "prompt": oneShot, "reply": reply.String(), "chat_id": savedID,
			})
			fmt.Println(string(b))
		}
		return nil
	}
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

	fmt.Printf("\nChatting with model %q. Type your message (Ctrl-D or /quit to exit; /help for commands).\n\n", model)

	reader := bufio.NewReader(os.Stdin)
	history := []map[string]any{}
	if strings.TrimSpace(*system) != "" {
		history = append(history, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
	}
	kbID := "" // knowledge base attached via /kb (F342)
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
		// REPL slash commands (F331).
		if strings.HasPrefix(line, "/") {
			fields := strings.Fields(line)
			cmd := fields[0]
			rest := strings.TrimSpace(strings.TrimPrefix(line, cmd))
			switch cmd {
			case "/help":
				fmt.Println("commands: /help /clear /system TEXT /model ID /kb ID /save [title] /quit")
			case "/clear":
				history = history[:0]
				if strings.TrimSpace(*system) != "" {
					history = append(history, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
				}
				fmt.Println("[history cleared]")
			case "/system":
				if rest == "" {
					fmt.Println("[usage: /system TEXT]")
					continue
				}
				*system = rest
				history = []map[string]any{{"role": "system", "content": rest}}
				fmt.Println("[system prompt set]")
			case "/model":
				if rest == "" {
					fmt.Println("[usage: /model ID]")
					continue
				}
				model = rest
				fmt.Printf("[model set to %q]\n", model)
			case "/kb":
				if rest == "" {
					if kbID == "" {
						fmt.Println("[no knowledge base attached — usage: /kb ID, /kb - to detach]")
					} else {
						fmt.Printf("[knowledge base %q attached]\n", kbID)
					}
					continue
				}
				if rest == "-" {
					kbID = ""
					fmt.Println("[knowledge base detached]")
					continue
				}
				// Validate the id against the KB list before attaching.
				code, out, err := cc.Get("/api/v1/knowledge-bases")
				if err != nil {
					fmt.Printf("[kb lookup failed: %v]\n", err)
					continue
				}
				if code != 200 {
					fmt.Printf("[kb lookup: %s]\n", out)
					continue
				}
				var kbs []struct {
					ID string `json:"id"`
				}
				json.Unmarshal(out, &kbs)
				found := false
				for _, k := range kbs {
					if k.ID == rest {
						found = true
					}
				}
				if !found {
					fmt.Printf("[no knowledge base %q — see llmctl kb list]\n", rest)
					continue
				}
				kbID = rest
				fmt.Printf("[knowledge base %q attached — top matches will be added to each message]\n", kbID)
			case "/save":
				title := rest
				if title == "" {
					title = "chat"
					for _, h := range history {
						if fmt.Sprint(h["role"]) == "user" {
							t := fmt.Sprint(h["content"])
							if len(t) > 40 {
								t = t[:40]
							}
							title = t
							break
						}
					}
				}
				msgs := []map[string]string{}
				for _, h := range history {
					msgs = append(msgs, map[string]string{"role": fmt.Sprint(h["role"]), "content": fmt.Sprint(h["content"])})
				}
				if len(msgs) == 0 {
					fmt.Println("[nothing to save]")
					continue
				}
				code, out, err := cc.Post("/api/v1/chats", map[string]any{
					"title": title, "model": model, "messages": msgs,
				})
				if err != nil {
					return err
				}
				if code != 200 {
					fmt.Printf("[save failed: %s]\n", out)
					continue
				}
				var saved struct {
					ID string `json:"id"`
				}
				json.Unmarshal(out, &saved)
				fmt.Printf("[saved as chat %s: %q]\n", saved.ID, title)
			default:
				fmt.Printf("[unknown command %q — try /help]\n", cmd)
			}
			continue
		}

		// Append user message.
		history = append(history, map[string]any{"role": "user", "content": line})

		// With a /kb attached, prepend the top KB matches for this message
		// as a context turn (same shape as the one-shot -kb flag, F342).
		reqMsgs := history
		if kbID != "" {
			ctxMsg, kerr := kbContextFor(cc, kbID, line)
			if kerr != nil {
				fmt.Printf("[kb search failed: %v]\n", kerr)
			} else if ctxMsg != "" {
				reqMsgs = make([]map[string]any, 0, len(history)+1)
				reqMsgs = append(reqMsgs, history[:len(history)-1]...)
				reqMsgs = append(reqMsgs, map[string]any{"role": "user", "content": ctxMsg})
				reqMsgs = append(reqMsgs, history[len(history)-1])
			}
		}

		body := map[string]any{
			"model":    model,
			"messages": reqMsgs,
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

// shortTime renders an RFC3339 timestamp as local "2006-01-02 15:04", or
// returns the input unchanged if it doesn't parse (F366).
func shortTime(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return rfc
	}
	return t.Local().Format("2006-01-02 15:04")
}
