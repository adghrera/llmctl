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
	"os/exec"
	"path/filepath"
	"runtime"
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
	case "tag":
		err = cmdInstanceTag(args)
	case "retag":
		err = cmdInstanceRetag(args)
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
	case "api":
		err = cmdApi(args)
	case "open":
		err = cmdOpen(args)
	case "version", "--version", "-v":
		versionJSON := func() string {
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
			return string(b)
		}
		if len(args) > 0 && (args[0] == "--json" || args[0] == "-json") { // F440
			fmt.Println(versionJSON())
			break
		}
		if len(args) > 1 && args[0] == "-out" { // F518: version JSON to a file
			if werr := os.WriteFile(args[1], []byte(versionJSON()+"\n"), 0o644); werr != nil {
				err = werr
			} else {
				fmt.Fprintf(os.Stderr, "wrote %s\n", args[1])
			}
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
  llmctl down [-json]
  llmctl status [-json] [-watch N] [-out FILE]
  llmctl doctor [-json] [--csv] [-quiet] [-check NAME]... [-watch N] [-out FILE]
  llmctl gpu [-json] [--csv] [-out FILE] [-watch N]
  llmctl jobs [-json] [--csv] [-watch N] [-out FILE]
  llmctl jobs wait <job-id> [-timeout N] [-interval N] [-json]
  llmctl jobs show <job-id> [-json] [-out FILE]
  llmctl jobs clear [-json]
  llmctl search <query> [-max N] [-json] [--csv] [-out FILE]
  llmctl structured -model ID -schema file.json [-key K] [-max-tokens N] [-temperature T] [-out FILE] <prompt>
  llmctl embed -model ID [-key K] [-json] [-out FILE] <text>
  llmctl complete -model ID [-key K] [-json] [-out FILE] <prompt>
  llmctl compare -models A,B [-system S] [-max-tokens N] [-temperature T] [-file PATH] [-json] [--csv] [-out FILE] <prompt>
  llmctl count <text> [-file F] [-chat ID] [-model ID] [-json] [-out FILE]   (estimate tokens, ~4 chars/token)
  llmctl bench -model ID [-n N] [-conc C] [-json|--csv] [-out FILE]
  llmctl bench -models A,B [-n N] [-conc C] [-json|--csv] [-out FILE]   (benchmark + comparison table)
  llmctl disk [-json] [--csv] [-out FILE] [-watch N]
  llmctl ps [-watch N] [--csv] [-json] [-out FILE] [-stats]   (-json/-out refresh each -watch tick, F620; -stats: per-model roll-up)
  llmctl backends list [-json|-csv|-out FILE]
  llmctl backends install <id> [--asset <hint>] [-json] [--wait]
  llmctl backends uninstall <id> [-json]
  llmctl backends add <plugin.json> [-json]
  llmctl backends remove <id> [-json]
  llmctl models list [-json] [--csv] [-out FILE]
  llmctl models show <id> [-json] [-out FILE]
  llmctl models probe [-json] [-out FILE]
  llmctl models ctx <id> <tokens> | price <id> <in/M> <out/M> | idle <id> <min|never> [-json|-out FILE]
  llmctl models note <id> [text…] [-json]   (free-text annotation; empty text clears)
  llmctl models tag <id> [a,b,c] [-json] | models retag <old> <new> | retag -rm <tag>   (free-form tags; list -tag X filters)
  llmctl models metadata <id> [-json] [-out FILE]
  llmctl models unused [--csv] [-out FILE]
  llmctl models stats [-json] [--csv] [-out FILE]   (per-backend roll-up: models, running, total size)
  llmctl models routable [-key K] [-json|-csv|-out FILE]   (what the gateway advertises on /v1/models)
  llmctl models add <id> <file> [-backend ID] [-ctx N] [-json] [-out FILE]
  llmctl models rename <id> <new-id> [-json]   (change a model id; aliases follow)
  llmctl models aliases [-json] [--csv] [-out FILE]
  llmctl models alias <name> <model-id> [-json]
  llmctl models unalias <name> [-json]
  llmctl models install <repo-or-url> [--file f.gguf] [--rev main] [--backend llama.cpp] [--id ID] [-json] [-out FILE] [--wait]
  llmctl models remove <id> [--file] [-json]
  llmctl models ctx <model-id> <tokens|0> [-json] [-out FILE]
  llmctl models price <model-id> <in-per-M> <out-per-M> [-json] [-out FILE]
  llmctl models hf-search <query> [-json] [-out FILE]
  llmctl models hf-files <repo> [-json] [-out FILE]
  llmctl start <model> [--backend ID] [--instance NAME] [--var k=v ...] [-json]
  llmctl stop <instance> [--force] | llmctl stop --all [--json]
  llmctl restart <instance> [-json] | llmctl restart --all [--json]
  llmctl maxparallel <instance> <n> [-json]   (0 = unlimited)
  llmctl note <instance> [text] [-json] (empty text clears) | autorestart <instance> <on|off> [-json] | setvars <instance> [-var k=v…] [-json]
  llmctl tag <instance> [a,b,c] [-json] (empty clears) | retag <old> <new> | retag -rm <tag>   (instance tags, F673)
  llmctl logs <instance> [--stream stderr|stdout] [--tail N] [-out FILE] [-f]
  llmctl logs -daemon [--tail N] [-json] [-out FILE] [-f]
  llmctl key generate [name] [-json] [-out FILE]
  llmctl key duplicate <id> [name] [-json]   (fresh key with the same restrictions)
  llmctl key show <id> [-json] [-out FILE]
  llmctl key note <id> <text...> [-json]
  llmctl key tag <id> [a,b] [-json] | llmctl key retag <old> <new> | retag -rm <tag> [-json]
  llmctl key update <id> [-name X] [-quota N] [-models a,b] … [-json]
  llmctl key disable <id> [-json] / llmctl key enable <id> [-json]
  llmctl key rotate <id> [-json] [-out FILE]
  llmctl key list [-json] [--csv] [-tag T] [-out FILE]
  llmctl key revoke <id> [-json]
  llmctl key reset-usage <id> [-json]
  llmctl autostart list [-json] [--csv] [-out FILE]
  llmctl autostart add <model> [--backend ID] [--instance NAME] [--port N] [--var k=v ...] [-json]
  llmctl autostart remove <model> [-json]
  llmctl autostart export [-out FILE] | autostart import <file.json> [-json]
  llmctl presets list [-json|-csv|-out FILE]|save -name N -model ID [...] [-json]|duplicate <name> <new-name> [-json]|start <name> [-json]|delete <name> [-json]|export|import [-json]
  llmctl catalog list [-json|-csv|-out FILE] | install <id> [-json] [--wait]
  llmctl scheduler list [-json|-csv|-out FILE] [-tag T]
  llmctl scheduler show <id> [-json] [-out FILE]
  llmctl scheduler add --name N --cron "0 9 * * *" [--kind chat|instance.start|instance.stop|backup] [--model ID] [--prompt TEXT] [--instance ID] [--kb ID] [-json]
  llmctl scheduler duplicate <id> [-name N] [-json] | run|enable|disable <id> | delete <id> [-json]
  llmctl scheduler tag <id> [a,b] [-json] | scheduler retag <old> <new> | retag -rm <tag> [-json]
  llmctl scheduler update <id> [-name N] [-cron C] [-model M] [-prompt P] [-enabled true|false] [-kb ID] [-json]
  llmctl scheduler runs [-n 20] [-json] [--csv] [-watch N] [-out FILE] [-stats]
  llmctl scheduler export [-out FILE] | scheduler import <file.json> [-json]
  llmctl requests [--model ID] [--status 4|404] [--min-ms N] [--key PREFIX] [--instance ID] [--id ID] [-n 20] [-json] [-watch N] [-stats [-by model|key|instance|day|hour|status]] [--csv on list and stats] [-out FILE]
  llmctl backups [list|create [-name LABEL] [-json]|restore <name> [-json]|download <name> [-json]|verify <name|-all> [-json]|prune [-keep N] [-json]|delete <name> [-json]] [-json|-csv|-out FILE|-watch N on list]
  llmctl usage [-n 14] [-key ID] [-keys] [-model ID] [--csv] [--json] [-out FILE] [-watch N]
  llmctl metrics [-watch N] [-model M] [--csv] [-json] [-out FILE]   (-json/-out refresh each -watch tick, F620)
  llmctl events [-n 20] [-kind K] [-model M] [-csv] [-json] [-out FILE] [-stats]
  llmctl webhooks [-n 20] [-event E] [--csv] [-json] [-watch N] [-out FILE] [-stats] | webhooks test [-event E] [-message M] [-json]   recent webhook delivery attempts / send a test event
  llmctl prompts [list] [filter] [-category X] [-json] [-csv] [-out FILE] [-stats] | llmctl prompts show <id> [-out file] | prompts render <id> [-input TEXT] [-var k=v…] [-out file] | prompts run <id> -model M [-input TEXT] [-var k=v…] [-system S] [-temperature T] [-max-tokens N] [-save TITLE | -chat ID] [-json] [-out FILE] | prompts categories [-json|-csv|-out FILE] | prompts add <id> -name N [-content T] [-json] | prompts duplicate <id> <new-id> [-name N] [-json] | prompts retag OLD NEW [-json] | prompts update <id> [-name N] [-json] | prompts delete <id> [-json] | prompts export | prompts import <file.json> [-json]
  llmctl chats [filter] [-n 20] [-tag T] [--csv] [-json] [-out FILE] [-stats] | chats list [filter] | chats show|search|export|export-all|import|fork [-json]|rename [-json]|pin|unpin [-json]|tag [-json]|retag [-json]|prune [-json]|delete [-json]|delete-all …
  llmctl skills [filter] [-json] [-csv] [-out FILE] | skills show [-out FILE]|add [-json]|update <id> [-name N] [-desc D] [-template T] [-tags a,b] [-json]|export|retag <old> <new> | retag -rm <tag> [-json]|enable|disable [-json]|delete <id> [-json]
  llmctl memory [filter] [-n 30] [-json|-csv|-out FILE] | memory show [-out FILE]|add [-json]|update [-json]|search|export|import [-json]|delete [-json]|retag OLD NEW | retag -rm TAG [-json] | memory -stats (per-tag roll-up) …
  llmctl kb [filter] [--csv] [-json] [-out FILE] | kb show|create [-json]|update [-json]|tag <id> [a,b]|retag <old> <new>|-rm <tag> [-json]|docs [-json|-csv|-out FILE]|doc|add-doc [-json]|export|search|search-all <q> [-limit N]|embed [-json]|delete [-json] …
  llmctl tools [-json|-csv|-out FILE] | tools show [-out FILE]|install <dir|url> [-id ID] [-json]|enable|disable [-json]|sandbox <id> <on|off> [-json]|run|delete <id> [-json]
  llmctl mcp [-json|-csv|-out FILE] | mcp add <name> <cmd> [-args 'a b'] [-env k=v …] [-json]|remove [-json]|enable|disable [-json]|test <id …> | mcp export | mcp import <file.json> [-json]
  llmctl cost [-keys] [-model M] [-key K] [--csv] [-json] [-out FILE] [-watch N]
  llmctl files [list|upload <path>|cat <id> [-out FILE]|delete <id>] [--csv] [-json] [-out FILE on list] [-stats on list]
  llmctl settings [-json] [-out FILE] | llmctl settings set <flags> [-json] | settings test-webhook [-event E] [-message M] [-json]
  llmctl batch -model ID [-system S] [-key K] [-concurrency N] [-temperature T] [-file F|piped stdin|prompts…] [-json] [--csv] [-out FILE]
  llmctl chat [--model ID] [--key KEY] [-prompt TEXT | piped stdin = one-shot] [-agent] [-save TITLE | -chat ID | -continue] [-kb ID] [-json] [-temperature T] [-max-tokens N] [-out FILE]
                                     interactive REPL supports /help /clear /system TEXT /model ID /save [title] /quit (F331)
  llmctl backup [file.tar.gz] [-json]     export state (config/chats/KBs/memory/…)
  llmctl backup verify <file> [-json]     check any local archive offline
  llmctl restore <file.tar.gz> [-json]    import a backup into the state dir
  llmctl version [--json] [-out FILE]
  llmctl completion [bash|zsh|fish|powershell]  print a shell completion script
  llmctl api <GET|POST|PUT|PATCH|DELETE> /path [-d JSON|-d-file F] [-out FILE]   raw management-API call
  llmctl open [route] [-print]   open the web UI (or one page, e.g. llmctl open requests) in your browser (-print just shows the URL)

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
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response JSON (F568)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Post("/api/v1/shutdown", nil)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("shutdown returned %d: %s", code, out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	fmt.Println("daemon stopped")
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
	watch := fs.Int("watch", 0, "re-run every N seconds and print checks as they change until Ctrl-C (0 = run once) (F557)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	type checkRow struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	fetch := func() (*struct {
		OK     bool       `json:"ok"`
		Checks []checkRow `json:"checks"`
	}, error) {
		code, out, err := cc.Get("/api/v1/doctor")
		if err != nil {
			return nil, err
		}
		if code != 200 {
			return nil, fmt.Errorf("doctor returned %d: %s", code, out)
		}
		var res struct {
			OK     bool       `json:"ok"`
			Checks []checkRow `json:"checks"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return nil, err
		}
		// -check filter (F259): keep only checks whose name contains one of the
		// given substrings; the exit status then reflects just those.
		if len(only) > 0 {
			var keep []checkRow
			for _, c := range res.Checks {
				for _, o := range only {
					if strings.Contains(c.Name, o) {
						keep = append(keep, c)
						break
					}
				}
			}
			if len(keep) == 0 {
				return nil, fmt.Errorf("no checks match: %s", strings.Join(only, ", "))
			}
			res.Checks = keep
			res.OK = true
			for _, c := range keep {
				if c.Status == "error" {
					res.OK = false
				}
			}
		}
		return &res, nil
	}
	res, err := fetch()
	if err != nil {
		return err
	}
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F464)
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		b = append(b, '\n')
		if *outFile != "" {
			if err := os.WriteFile(*outFile, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
		} else {
			fmt.Println(string(b))
		}
		if !res.OK {
			return fmt.Errorf("doctor: some checks failed")
		}
		return nil
	}
	sym := map[string]string{"ok": "[ok]  ", "warn": "[warn]", "error": "[fail]"}
	printCheck := func(c checkRow) {
		s, ok := sym[c.Status]
		if !ok {
			s = c.Status
		}
		fmt.Printf("%s %-9s %s\n", s, c.Name, c.Detail)
	}
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
		printCheck(c)
	}
	if *watch > 0 {
		// Live health tail: print only checks whose status/detail changes (F557).
		all := func(rs []checkRow) map[string][2]string {
			m := map[string][2]string{}
			for _, c := range rs {
				m[c.Name] = [2]string{c.Status, c.Detail}
			}
			return m
		}
		prev := all(res.Checks)
		quietOK := warns == 0 && res.OK
		for _, c := range res.Checks {
			if c.Status != "ok" {
				quietOK = false
			}
		}
		fmt.Printf("\nwatching health every %ds (Ctrl-C to stop)…\n", *watch)
		for {
			time.Sleep(time.Duration(*watch) * time.Second)
			cur, err := fetch()
			if err != nil {
				return err
			}
			now := all(cur.Checks)
			var changed []checkRow
			for _, c := range cur.Checks {
				if p, ok := prev[c.Name]; !ok || p != [2]string{c.Status, c.Detail} {
					changed = append(changed, c)
				}
			}
			for name := range prev {
				if _, ok := now[name]; !ok {
					fmt.Printf("        %-9s (removed)\n", name)
				}
			}
			for _, c := range changed {
				fmt.Printf("%s %s %-9s %s\n", time.Now().Local().Format("15:04:05"), sym[c.Status], c.Name, c.Detail)
			}
			nWarn := 0
			for _, c := range cur.Checks {
				if c.Status != "ok" {
					nWarn++
				}
			}
			if len(changed) == 0 && !quietOK && cur.OK && nWarn == 0 {
				fmt.Printf("%s all checks passed\n", time.Now().Local().Format("15:04:05"))
			}
			quietOK = cur.OK && nWarn == 0
			prev = now
			res = cur
		}
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
// cmdGPU lists detected GPUs; -watch N reprints every N seconds until
// interrupted (F593), handy while instances load and unload.
func cmdGPU(args []string) error {
	watch := 0
	for i, a := range args {
		if a == "-watch" && i+1 < len(args) {
			watch, _ = strconv.Atoi(args[i+1])
		} else if strings.HasPrefix(a, "-watch=") {
			watch, _ = strconv.Atoi(strings.TrimPrefix(a, "-watch="))
		}
	}
	if watch <= 0 {
		return gpuOnce(args)
	}
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live GPUs — refreshing every %ds (Ctrl-C to stop)\n\n", watch)
		if err := gpuOnce(args); err != nil {
			return err
		}
		time.Sleep(time.Duration(watch) * time.Second)
	}
}

func gpuOnce(args []string) error {
	fs := flag.NewFlagSet("gpu", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw GPU JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the GPU list as CSV (F430)")
	outFile := fs.String("out", "", "write the raw GPU JSON to this file instead of stdout (F463)")
	fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once) (F593)")
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
// cmdDisk reports model/storage disk usage; -watch N reprints every N seconds
// until interrupted (F590), pairing with usage/cost -watch.
func cmdDisk(args []string) error {
	watch := 0
	for i, a := range args {
		if a == "-watch" && i+1 < len(args) {
			watch, _ = strconv.Atoi(args[i+1])
		} else if strings.HasPrefix(a, "-watch=") {
			watch, _ = strconv.Atoi(strings.TrimPrefix(a, "-watch="))
		}
	}
	if watch <= 0 {
		return diskOnce(args)
	}
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live disk — refreshing every %ds (Ctrl-C to stop)\n\n", watch)
		if err := diskOnce(args); err != nil {
			return err
		}
		time.Sleep(time.Duration(watch) * time.Second)
	}
}

func diskOnce(args []string) error {
	fs := flag.NewFlagSet("disk", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw disk JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the disk report as CSV (F451)")
	outFile := fs.String("out", "", "write the raw disk JSON to this file instead of stdout (F465)")
	fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once) (F590)")
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
		return fmt.Errorf("usage: llmctl presets <list|save|duplicate|start|delete|export|import> [flags]")
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
		pifs := flag.NewFlagSet("presets import", flag.ExitOnError)
		pijson := pifs.Bool("json", false, "print {\"created\",\"skipped\"} as JSON (F542)")
		pifs.Parse(reorderFlags(rest))
		if pifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl presets import <file.json> [-json]")
		}
		data, err := os.ReadFile(pifs.Arg(0))
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
				if *pijson {
					fmt.Fprintf(os.Stderr, "  skip %v: %s\n", p["name"], out)
				} else {
					fmt.Printf("  skip %v: %s\n", p["name"], out)
				}
				skipped++
				continue
			}
			created++
		}
		if *pijson {
			b, _ := json.Marshal(map[string]int{"created": created, "skipped": skipped})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("imported %d presets (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("presets list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw presets JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the preset list as CSV (F386)")
		outFile := fs.String("out", "", "write the raw presets JSON to this file instead of stdout (F493)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/presets")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("presets returned %d: %s", code, out)
		}
		if *outFile != "" { // F493: raw JSON to a file, stdout stays clean
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
		jsonOut := fs.Bool("json", false, "print the raw response JSON (F524)")
		var vars stringList
		fs.Var(&vars, "var", "parameter k=v (repeatable)")
		fs.Parse(reorderFlags(rest))
		if *name == "" || *model == "" {
			return fmt.Errorf("usage: llmctl presets save -name N -model ID [-instance I] [-backend B] [-port P] [-var k=v ...] [-json]")
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
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("preset %s saved\n", *name)
		return nil
	case "duplicate":
		// Copy an existing preset under a new name (F648).
		dfs := flag.NewFlagSet("presets duplicate", flag.ExitOnError)
		djson := dfs.Bool("json", false, "print the raw response JSON")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() != 2 {
			return fmt.Errorf("usage: llmctl presets duplicate <name> <new-name> [-json]")
		}
		code, out, err := cc.Get("/api/v1/presets")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("presets duplicate: %s", out)
		}
		var list []map[string]any
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		var body map[string]any
		for _, p := range list {
			if p["name"] != dfs.Arg(0) {
				continue
			}
			body = map[string]any{"name": dfs.Arg(1), "modelId": p["modelId"]}
			if v, _ := p["instance"].(string); v != "" {
				body["instance"] = v
			}
			if v, _ := p["backendId"].(string); v != "" {
				body["backendId"] = v
			}
			if v, _ := p["port"].(float64); v > 0 {
				body["port"] = int(v)
			}
			if v, ok := p["vars"].(map[string]any); ok && len(v) > 0 {
				body["vars"] = v
			}
			break
		}
		if body == nil {
			return fmt.Errorf("unknown preset: %s", dfs.Arg(0))
		}
		code, out, err = cc.Post("/api/v1/presets", body)
		if err != nil {
			return err
		}
		if code != 200 && code != 201 {
			return fmt.Errorf("presets duplicate returned %d: %s", code, out)
		}
		if *djson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("preset %s duplicated from %s\n", dfs.Arg(1), dfs.Arg(0))
		return nil
	case "start":
		fs := flag.NewFlagSet("presets start", flag.ExitOnError)
		port := fs.Int("port", 0, "override the preset port")
		jsonOut := fs.Bool("json", false, "print the raw started-instance JSON (F525)")
		var vars stringList
		fs.Var(&vars, "var", "parameter k=v override (repeatable)")
		fs.Parse(reorderFlags(rest))
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl presets start <name> [-port P] [-var k=v ...] [-json]")
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
		if *jsonOut {
			fmt.Print(string(out))
			return nil
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
		pdfs := flag.NewFlagSet("presets delete", flag.ExitOnError)
		pdjson := pdfs.Bool("json", false, "print the raw response JSON (F538)")
		pdfs.Parse(reorderFlags(rest))
		if pdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl presets delete <name> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/presets/" + url.PathEscape(pdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("presets delete returned %d: %s", code, out)
		}
		if *pdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("preset %s deleted\n", pdfs.Arg(0))
		return nil
	default:
		return fmt.Errorf("unknown presets subcommand: %s (list|save|duplicate|start|delete)", sub)
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
		outFile := fs.String("out", "", "write the raw catalog JSON to this file instead of stdout (F497)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/catalog")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("catalog returned %d: %s", code, out)
		}
		if *outFile != "" { // F497: raw JSON to a file, stdout stays clean
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
		cifs := flag.NewFlagSet("catalog install", flag.ExitOnError)
		cijson := cifs.Bool("json", false, "print the raw response JSON (F541)")
		ciwait := cifs.Bool("wait", false, "block until the download finishes (F576)")
		cifs.Parse(reorderFlags(rest))
		if cifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl catalog install <id> [-json] [--wait]  (see: llmctl catalog list)")
		}
		code, out, err := cc.Post("/api/v1/catalog/install", map[string]any{"id": cifs.Arg(0)})
		if err != nil {
			return err
		}
		if code != 200 && code != 202 {
			return fmt.Errorf("catalog install returned %d: %s", code, out)
		}
		if *cijson {
			fmt.Print(string(out))
			if *ciwait {
				var cj struct {
					Job string `json:"job"`
				}
				json.Unmarshal(out, &cj)
				return waitJob(cj.Job, true)
			}
			return nil
		}
		var cj struct {
			Job   string `json:"job"`
			Model string `json:"model"`
		}
		json.Unmarshal(out, &cj)
		fmt.Printf("catalog install %s started (job %s) — track: llmctl jobs wait %s\n", cifs.Arg(0), cj.Job, cj.Job)
		if *ciwait {
			return waitJob(cj.Job, false)
		}
		return nil
	default:
		return fmt.Errorf("unknown catalog subcommand: %s (list|install)", sub)
	}
}

// cmdMaxParallel caps how many simultaneous gateway requests an instance
// serves (PATCH /api/v1/instances/{id}/maxparallel, F270).
func cmdMaxParallel(args []string) error {
	fs := flag.NewFlagSet("maxparallel", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response JSON (F532)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 2 {
		return fmt.Errorf("usage: llmctl maxparallel <instance-id> <n> [-json]  (0 = unlimited)")
	}
	n, err := strconv.Atoi(fs.Arg(1))
	if err != nil || n < 0 {
		return fmt.Errorf("invalid maxParallel: %s (must be >= 0)", fs.Arg(1))
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Do("PATCH", "/api/v1/instances/"+url.PathEscape(fs.Arg(0))+"/maxparallel",
		map[string]any{"maxParallel": n})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("maxparallel: %s", out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	label := "unlimited"
	if n > 0 {
		label = strconv.Itoa(n)
	}
	fmt.Printf("instance %s now serves at most %s concurrent request(s)\n", fs.Arg(0), label)
	return nil
}

// cmdNote sets the free-text note on an instance (PATCH note endpoint, F119).
func cmdNote(args []string) error {
	fs := flag.NewFlagSet("note", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response JSON (F529)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: llmctl note <instance-id> [text…] [-json]  (empty text clears)")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	note := strings.Join(fs.Args()[1:], " ")
	code, out, err := cc.Do("PATCH", "/api/v1/instances/"+url.PathEscape(fs.Arg(0))+"/note",
		map[string]any{"note": note})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("note: %s", out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	if strings.TrimSpace(note) == "" {
		fmt.Printf("instance %s note cleared\n", fs.Arg(0))
		return nil
	}
	fmt.Printf("instance %s note set\n", fs.Arg(0))
	return nil
}

// cmdInstanceTag replaces an instance's tags (PATCH tags endpoint, F673).
func cmdInstanceTag(args []string) error {
	fs := flag.NewFlagSet("tag", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response JSON")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: llmctl tag <instance-id> [a,b,c] [-json]  (empty clears)")
	}
	var tags []string
	if fs.NArg() > 1 {
		for _, t := range strings.Split(fs.Arg(1), ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Do("PATCH", "/api/v1/instances/"+url.PathEscape(fs.Arg(0))+"/tags",
		map[string]any{"tags": tags})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("tag: %s", out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	if len(tags) == 0 {
		fmt.Printf("instance %s tags cleared\n", fs.Arg(0))
	} else {
		fmt.Printf("instance %s tags: %s\n", fs.Arg(0), strings.Join(tags, ", "))
	}
	return nil
}

// cmdInstanceRetag bulk-renames or drops a tag across all instances (F673),
// twin of `models retag`; the tag rules live in the tags endpoint.
func cmdInstanceRetag(args []string) error {
	fs := flag.NewFlagSet("retag", flag.ExitOnError)
	rm := fs.Bool("rm", false, "drop the tag instead of renaming it")
	jsonOut := fs.Bool("json", false, "print the count as JSON")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 1 || (!*rm && fs.NArg() < 2) {
		return fmt.Errorf("usage: llmctl retag <old-tag> <new-tag> | llmctl retag -rm <tag> [-json]")
	}
	oldTag, newTag := fs.Arg(0), fs.Arg(1)
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/instances")
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("instances returned %d: %s", code, out)
	}
	var is []struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(out, &is); err != nil {
		return err
	}
	changed := 0
	for _, i := range is {
		found := false
		var tags []string
		for _, t := range i.Tags {
			if t != oldTag {
				tags = append(tags, t)
				continue
			}
			found = true
			if !*rm {
				tags = append(tags, newTag)
			}
		}
		if !found {
			continue
		}
		code, out, err := cc.Do("PATCH", "/api/v1/instances/"+url.PathEscape(i.ID)+"/tags",
			map[string]any{"tags": tags})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("retag %s: %s", i.ID, out)
		}
		changed++
	}
	if *jsonOut {
		fmt.Printf("{\"changed\": %d}\n", changed)
		return nil
	}
	if *rm {
		fmt.Printf("removed tag %q from %d instance(s)\n", oldTag, changed)
	} else {
		fmt.Printf("renamed tag %q -> %q on %d instance(s)\n", oldTag, newTag, changed)
	}
	return nil
}

// cmdAutoRestartToggle toggles crash auto-restart on an instance (F087).
func cmdAutoRestartToggle(args []string) error {
	fs := flag.NewFlagSet("autorestart", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response JSON (F530)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 2 {
		return fmt.Errorf("usage: llmctl autorestart <instance-id> <on|off> [-json]")
	}
	on := strings.ToLower(fs.Arg(1)) == "on"
	if s := strings.ToLower(fs.Arg(1)); s != "on" && s != "off" {
		return fmt.Errorf("expected on or off, got %q", fs.Arg(1))
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Post("/api/v1/instances/"+url.PathEscape(fs.Arg(0))+"/autorestart",
		map[string]any{"enabled": on})
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("autorestart: %s", out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	state := "off"
	if on {
		state = "on"
	}
	fmt.Printf("instance %s auto-restart %s\n", fs.Arg(0), state)
	return nil
}

// cmdSetVars replaces the stored start vars of an instance (PUT vars, F091).
func cmdSetVars(args []string) error {
	fs := flag.NewFlagSet("setvars", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw response JSON (F531)")
	var vars stringList
	fs.Var(&vars, "var", "parameter k=v (repeatable)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl setvars <instance-id> [-var k=v ...] [-json]")
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
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	fmt.Printf("instance %s vars updated (restart to apply)\n", fs.Arg(0))
	return nil
}

// cmdJobs lists background jobs (model/backend installs) tracked by the
// daemon (GET /api/v1/jobs, F273).
func cmdJobs(args []string) error {
	if len(args) > 0 && args[0] == "wait" { // F575: block until a job finishes
		return cmdJobsWait(args[1:])
	}
	if len(args) > 0 && args[0] == "show" { // F578: one job in detail
		return cmdJobsShow(args[1:])
	}
	if len(args) > 0 && args[0] == "clear" { // F585: drop finished jobs
		cfs := flag.NewFlagSet("jobs clear", flag.ExitOnError)
		cjson := cfs.Bool("json", false, "print the raw {cleared} JSON (for scripts)")
		cfs.Parse(reorderFlags(args[1:]))
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Do("DELETE", "/api/v1/jobs", nil)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("jobs clear returned %d: %s", code, out)
		}
		if *cjson {
			fmt.Print(string(out))
			return nil
		}
		var res struct {
			Cleared int `json:"cleared"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		fmt.Printf("cleared %d finished job(s)\n", res.Cleared)
		return nil
	}
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

// cmdJobsWait blocks until one job reaches a terminal state (F575). Exit is
// non-zero when the job failed, so scripts can chain: install … && wait.
func cmdJobsWait(args []string) error {
	fs := flag.NewFlagSet("jobs wait", flag.ExitOnError)
	timeout := fs.Int("timeout", 0, "give up after N seconds (0 = wait until done)")
	interval := fs.Int("interval", 1, "poll every N seconds")
	jsonOut := fs.Bool("json", false, "print the final job JSON instead of progress (for scripts)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl jobs wait <job-id> [-timeout N] [-interval N] [-json]")
	}
	id := fs.Arg(0)
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	var deadline time.Time
	if *timeout > 0 {
		deadline = time.Now().Add(time.Duration(*timeout) * time.Second)
	}
	type jobRow struct {
		ID       string  `json:"id"`
		Kind     string  `json:"kind"`
		Target   string  `json:"target"`
		Status   string  `json:"status"`
		Progress float64 `json:"progress"`
		Message  string  `json:"message"`
		Error    string  `json:"error"`
	}
	last := ""
	for {
		code, out, err := cc.Get("/api/v1/jobs/" + url.PathEscape(id))
		if err != nil {
			return err
		}
		if code == 404 {
			return fmt.Errorf("no such job: %s (jobs live in daemon memory — see 'llmctl jobs')", id)
		}
		if code != 200 {
			return fmt.Errorf("jobs wait returned %d: %s", code, out)
		}
		var j jobRow
		if err := json.Unmarshal(out, &j); err != nil {
			return err
		}
		switch j.Status {
		case "done":
			if *jsonOut {
				fmt.Print(string(out))
			} else {
				fmt.Printf("job %s done (%s %s)\n", j.ID, j.Kind, j.Target)
			}
			return nil
		case "error":
			if *jsonOut {
				fmt.Print(string(out))
			}
			return fmt.Errorf("job %s failed: %s", j.ID, j.Error)
		}
		if !*jsonOut { // print progress only when it actually changed
			line := fmt.Sprintf("%s %3.0f%% %s", j.Status, j.Progress*100, j.Message)
			if line != last {
				fmt.Println(line)
				last = line
			}
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return fmt.Errorf("job %s still %s after %ds", j.ID, j.Status, *timeout)
		}
		time.Sleep(time.Duration(*interval) * time.Second)
	}
}

// cmdJobsShow prints one tracked job in detail (F578), the human twin of the
// F575 single-job endpoint.
func cmdJobsShow(args []string) error {
	fs := flag.NewFlagSet("jobs show", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw job JSON (for scripts)")
	outFile := fs.String("out", "", "write the raw job JSON to this file instead of stdout (F621)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl jobs show <job-id> [-json] [-out FILE]")
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/jobs/" + url.PathEscape(fs.Arg(0)))
	if err != nil {
		return err
	}
	if code == 404 {
		return fmt.Errorf("no such job: %s (jobs live in daemon memory — see 'llmctl jobs')", fs.Arg(0))
	}
	if code != 200 {
		return fmt.Errorf("jobs show returned %d: %s", code, out)
	}
	if *outFile != "" { // F621: raw JSON to a file, stdout stays clean
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
	var j struct {
		ID       string    `json:"id"`
		Kind     string    `json:"kind"`
		Target   string    `json:"target"`
		Status   string    `json:"status"`
		Progress float64   `json:"progress"`
		Message  string    `json:"message"`
		Error    string    `json:"error"`
		Created  time.Time `json:"created"`
		Updated  time.Time `json:"updated"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return err
	}
	fmt.Printf("id:       %s\nkind:     %s\ntarget:   %s\nstatus:   %s\nprogress: %.0f%%\n",
		j.ID, j.Kind, j.Target, j.Status, j.Progress*100)
	if j.Message != "" {
		fmt.Printf("message:  %s\n", j.Message)
	}
	if j.Error != "" {
		fmt.Printf("error:    %s\n", j.Error)
	}
	fmt.Printf("created:  %s\nupdated:  %s\n",
		j.Created.Local().Format(time.RFC3339), j.Updated.Local().Format(time.RFC3339))
	return nil
}

// waitJob blocks on cmdJobsWait with default timing; used by the install
// commands' --wait flag (F576).
func waitJob(jobID string, jsonOut bool) error {
	if jsonOut {
		return cmdJobsWait([]string{jobID, "-json"})
	}
	return cmdJobsWait([]string{jobID})
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
	csvOut := fs.Bool("csv", false, "output one CSV row per model — model,ok,reply,error (implies -json, F606)")
	outFile := fs.String("out", "", "write the JSON array to this file instead of stdout (implies -json, F454)")
	fileFlag := fs.String("file", "", "prepend a local file's contents as context to every model (like chat -file, F670)")
	fs.Parse(reorderFlags(args))
	if *outFile != "" || *csvOut {
		*jsonOut = true // -out and --csv both work off the collected results
	}
	if *modelsFlag == "" {
		return fmt.Errorf("usage: llmctl compare -models A,B [-key K] [-system S] [-max-tokens N] [-temperature T] [-file PATH] <prompt>  (or pipe stdin)")
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
	fileCtx := ""
	if *fileFlag != "" {
		fileCtx, err = fileContextFor(*fileFlag)
		if err != nil {
			return err
		}
	}
	for _, m := range strings.Split(*modelsFlag, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		msgs := []map[string]string{}
		if strings.TrimSpace(*system) != "" {
			msgs = append(msgs, map[string]string{"role": "system", "content": strings.TrimSpace(*system)})
		}
		if fileCtx != "" { // file context before the prompt, like chat -file (F670)
			msgs = append(msgs, map[string]string{"role": "user", "content": fileCtx})
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
		if *csvOut { // F606: one row per model, full reply text
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"model", "ok", "reply", "error"})
			for _, r := range results {
				_ = w.Write([]string{r.Model, strconv.FormatBool(r.Error == ""), r.Reply, r.Error})
			}
			w.Flush()
			return w.Error()
		}
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
	"compare", "count", "bench", "note", "tag", "retag", "autorestart", "setvars", "ps",
	"backends", "models", "start", "stop", "restart", "logs", "key",
	"autostart", "requests", "backups", "usage", "metrics", "events",
	"webhooks", "prompts", "chats", "skills", "memory", "kb", "tools",
	"mcp", "cost", "files", "settings", "batch", "scheduler", "chat",
	"backup", "restore", "version", "help", "completion", "api", "open",
}

// llmSubCommands lists the subcommands of each top-level command that has
// them, used by shell completion (F350). Keys must match llmTopCommands.
var llmSubCommands = map[string]string{
	"autostart": "list add remove export import",
	"backends":  "list install uninstall add remove",
	"backups":   "list create restore verify prune download delete",
	"catalog":   "list install",
	"chats":     "list show search export export-all import fork rename pin unpin tag retag prune delete delete-all",
	"files":     "list upload cat delete",
	"jobs":      "wait show clear",
	"kb":        "list show create update tag retag docs doc add-doc export search search-all embed delete",
	"key":       "generate duplicate show note tag retag update enable disable rotate list revoke prune reset-usage",
	"mcp":       "list add remove enable disable test export import",
	"memory":    "list show add update search delete export import retag",
	"models":    "list stats show add rename note tag retag probe price ctx idle metadata install remove routable unused hf-search hf-files aliases alias unalias",
	"presets":   "list save duplicate start delete export import",
	"prompts":   "list show render run categories add duplicate retag update delete export import",
	"scheduler": "list show add duplicate update run runs enable disable tag retag delete export import",
	"settings":  "set test-webhook",
	"skills":    "list show add update export retag enable disable delete",
	"tools":     "list show install enable disable sandbox run delete",
}

// llmIDSources maps a top-level command to a shell pipeline that prints its
// entity ids (one per line) from the running daemon; used for dynamic id
// completion (F550). Keys must match llmTopCommands.
var llmIDSources = map[string]string{
	"start":       "llmctl models list --csv 2>/dev/null | tail -n +2 | cut -d, -f1; llmctl models aliases --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"stop":        "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"restart":     "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"logs":        "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"note":        "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"tag":         "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"autorestart": "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"setvars":     "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"maxparallel": "llmctl ps --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"backups":     "llmctl backups list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"chats":       "llmctl chats list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"key":         "llmctl key list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"kb":          "llmctl kb list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"mcp":         "llmctl mcp list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"memory":      "llmctl memory list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"models":      "llmctl models list --csv 2>/dev/null | tail -n +2 | cut -d, -f1; llmctl models aliases --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"prompts":     "llmctl prompts list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"scheduler":   "llmctl scheduler list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"skills":      "llmctl skills list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
	"tools":       "llmctl tools list --csv 2>/dev/null | tail -n +2 | cut -d, -f1",
}

// llmIDCommands lists the top-level commands that take an entity id directly
// (no subcommand), so the id is completed at word position 2.
const llmIDCommands = "start|stop|restart|logs|note|autorestart|setvars|maxparallel"

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
		var ids strings.Builder
		idNames := make([]string, 0, len(llmIDSources))
		for k := range llmIDSources {
			idNames = append(idNames, k)
		}
		sort.Strings(idNames)
		for _, n := range idNames {
			fmt.Fprintf(&ids, "    %s) %s ;;\n", n, llmIDSources[n])
		}
		fmt.Printf(`_llmctl_ids() {
  case "$1" in
%s  esac
}
_llmctl_completions() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
    return
  fi
  case "${COMP_WORDS[1]}" in
    %s)
      if [ "$COMP_CWORD" -eq 2 ]; then
        COMPREPLY=( $(compgen -W "$(_llmctl_ids "${COMP_WORDS[1]}")" -- "$cur") )
      fi
      return ;;
  esac
  if [ "$COMP_CWORD" -eq 3 ]; then
    local ids
    ids=$(_llmctl_ids "${COMP_WORDS[1]}")
    if [ -n "$ids" ]; then
      COMPREPLY=( $(compgen -W "$ids" -- "$cur") )
      return
    fi
  fi
  local subs=""
  case "${COMP_WORDS[1]}" in
%s  esac
  COMPREPLY=( $(compgen -W "$subs" -- "$cur") )
}
complete -F _llmctl_completions llmctl
`, ids.String(), cmds, llmIDCommands, sb.String())
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
		var ids strings.Builder
		idNames := make([]string, 0, len(llmIDSources))
		for k := range llmIDSources {
			idNames = append(idNames, k)
		}
		sort.Strings(idNames)
		for _, n := range idNames {
			fmt.Fprintf(&ids, "      %s) %s ;;\n", n, llmIDSources[n])
		}
		fmt.Printf(`#compdef llmctl
_llmctl_ids() {
  case "$1" in
%s  esac
}
_llmctl() {
  local -a cmds
  cmds=(
%s  )
  if (( CURRENT == 2 )); then
    _describe 'command' cmds
    return
  fi
  case "${words[2]}" in
    %s)
      if (( CURRENT == 3 )); then
        local -a ids; ids=(${(f)"$(_llmctl_ids ${words[2]})"})
        (( ${#ids} > 0 )) && _describe 'id' ids
      fi
      return ;;
  esac
  if (( CURRENT == 4 )); then
    local -a ids; ids=(${(f)"$(_llmctl_ids ${words[2]})"})
    (( ${#ids} > 0 )) && { _describe 'id' ids; return; }
  fi
  local -a subs
  case "${words[2]}" in
%s  esac
  (( ${#subs} > 0 )) && _describe 'subcommand' subs
}
compdef _llmctl llmctl
`, ids.String(), quoted, llmIDCommands, sb.String())
	case "fish":
		// Fish: one `complete` line per command (level 1) and per command's
		// subcommand set (level 2) (F354). Direct-id commands also get a
		// dynamic id list from the running daemon (F550).
		var sb strings.Builder
		for _, n := range names {
			fmt.Fprintf(&sb, "complete -c llmctl -n \"__fish_seen_subcommand_from %s\" -a \"%s\"\n", n, llmSubCommands[n])
		}
		var idf strings.Builder
		for _, c := range strings.Split(llmIDCommands, "|") {
			fmt.Fprintf(&idf, "complete -c llmctl -n \"__fish_seen_subcommand_from %s\" -a \"(%s)\"\n", c, llmIDSources[c])
		}
		fmt.Printf(`complete -c llmctl -f
complete -c llmctl -n __fish_use_subcommand -a "%s"
%s%s`, cmds, sb.String(), idf.String())
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
	stats := fs.Bool("stats", false, "aggregate the instances per model instead of listing them (F629)")
	tagF := fs.String("tag", "", "only instances carrying this tag (F675)")
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
		if *tagF != "" { // F675: exact tag filter, twin of models list -tag;
			// applied before every output mode so -json/-csv/-out/-stats agree
			var insts []map[string]any
			if err := json.Unmarshal(out, &insts); err != nil {
				return err
			}
			keep := insts[:0]
			for _, i := range insts {
				tags, _ := i["tags"].([]any)
				for _, t := range tags {
					if t == *tagF {
						keep = append(keep, i)
						break
					}
				}
			}
			insts = keep
			if out, err = json.Marshal(insts); err != nil {
				return err
			}
		}
		if *stats { // F629: one row per model (replicas, running, served, restarts)
			var insts []struct {
				ModelID      string `json:"modelId"`
				Status       string `json:"status"`
				Served       int64  `json:"served"`
				RestartCount int    `json:"restartCount"`
			}
			if err := json.Unmarshal(out, &insts); err != nil {
				return err
			}
			type pst struct {
				Model    string `json:"model"`
				Replicas int    `json:"replicas"`
				Running  int    `json:"running"`
				Served   int64  `json:"served"`
				Restarts int    `json:"restarts"`
			}
			byModel := map[string]*pst{}
			for _, i := range insts {
				g := byModel[i.ModelID]
				if g == nil {
					g = &pst{Model: i.ModelID}
					byModel[i.ModelID] = g
				}
				g.Replicas++
				if i.Status == "running" {
					g.Running++
				}
				g.Served += i.Served
				g.Restarts += i.RestartCount
			}
			names := make([]string, 0, len(byModel))
			for k := range byModel {
				names = append(names, k)
			}
			sort.Slice(names, func(i, j int) bool { return byModel[names[i]].Served > byModel[names[j]].Served })
			rows := make([]pst, 0, len(names))
			for _, k := range names {
				rows = append(rows, *byModel[k])
			}
			if *outFile != "" {
				b, _ := json.Marshal(rows)
				if err := os.WriteFile(*outFile, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
				return nil
			}
			if *jsonOut {
				b, _ := json.Marshal(rows)
				fmt.Println(string(b))
				return nil
			}
			if *csvOut {
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"model", "replicas", "running", "served", "restarts"})
				for _, r := range rows {
					_ = w.Write([]string{r.Model, strconv.Itoa(r.Replicas), strconv.Itoa(r.Running),
						strconv.FormatInt(r.Served, 10), strconv.Itoa(r.Restarts)})
				}
				w.Flush()
				return w.Error()
			}
			if len(rows) == 0 {
				fmt.Println("no instances (start one: llmctl start <model-id>)")
				return nil
			}
			fmt.Printf("%-24s %9s %8s %8s %9s\n", "MODEL", "REPLICAS", "RUNNING", "SERVED", "RESTARTS")
			for _, r := range rows {
				fmt.Printf("%-24s %9d %8d %8d %9d\n", r.Model, r.Replicas, r.Running, r.Served, r.Restarts)
			}
			return nil
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
	if *watch <= 0 || *csvOut || *stats { // CSV/stats are one-shot exports (F373, F629)
		return printOnce()
	}
	// Live view: clear + reprint every interval until interrupted (F194).
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live instances — refreshing every %ds (Ctrl-C to stop)\n\n", *watch)
		if err := printOnce(); err != nil { // F620: -json/-out honored each tick
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
		Tags         []string  `json:"tags"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		if csvOut { // keep the stream parseable (F373)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "model", "backend", "transport", "port", "status", "uptime_s", "served", "restarts", "note", "tags"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no instances (start one: llmctl start <model-id>)")
		return nil
	}
	if csvOut { // F373
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "model", "backend", "transport", "port", "status", "uptime_s", "served", "restarts", "note", "tags"})
		for _, i := range list {
			up := int64(0)
			if i.Status == "running" && !i.StartedAt.IsZero() {
				up = int64(time.Since(i.StartedAt).Seconds())
			}
			_ = w.Write([]string{i.ID, i.ModelID, i.BackendID, i.Transport, strconv.Itoa(i.Port), i.Status,
				strconv.FormatInt(up, 10), strconv.FormatInt(i.Served, 10), strconv.Itoa(i.RestartCount), i.Note,
				strings.Join(i.Tags, ";")})
		}
		w.Flush()
		return w.Error()
	}
	fmt.Printf("%-28s %-24s %-12s %-8s %-7s %-9s %-8s %6s %8s %-40s %s\n", "INSTANCE", "MODEL", "BACKEND", "TRANS", "PORT", "STATUS", "UPTIME", "SERVED", "RESTARTS", "NOTE", "TAGS")
	for _, i := range list {
		up := ""
		if i.Status == "running" && !i.StartedAt.IsZero() {
			up = uptimeShort(time.Since(i.StartedAt))
		}
		note := i.Note
		if len(note) > 40 {
			note = note[:37] + "..."
		}
		fmt.Printf("%-28s %-24s %-12s %-8s %-7v %-9s %-8s %6d %8d %-40s %s\n",
			i.ID, i.ModelID, i.BackendID, i.Transport, i.Port, i.Status, up, i.Served, i.RestartCount, note, strings.Join(i.Tags, ","))
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
		outFile := fs.String("out", "", "write the raw backends JSON to this file instead of stdout (F496)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/backends")
		if err != nil {
			return err
		}
		if *outFile != "" { // F496: raw JSON to a file, stdout stays clean
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
		bifs := flag.NewFlagSet("backends install", flag.ExitOnError)
		bijson := bifs.Bool("json", false, "print the raw job JSON (F566)")
		biwait := bifs.Bool("wait", false, "block until the install finishes (F576)")
		asset := bifs.String("asset", "", "asset hint")
		bifs.Parse(reorderFlags(rest))
		if bifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backends install <id> [--asset hint] [-json] [--wait]")
		}
		path := "/api/v1/backends/" + bifs.Arg(0) + "/install"
		if *asset != "" {
			path += "?asset=" + *asset
		}
		code, out, err := cc.Post(path, nil)
		if err != nil {
			return err
		}
		if code != 202 {
			return fmt.Errorf("install returned %d: %s", code, out)
		}
		if *bijson {
			fmt.Print(string(out))
			if *biwait {
				var bj struct {
					Job string `json:"job"`
				}
				json.Unmarshal(out, &bj)
				return waitJob(bj.Job, true)
			}
			return nil
		}
		var bj struct {
			Job string `json:"job"`
		}
		json.Unmarshal(out, &bj)
		fmt.Printf("backend %s install started (job %s) — track: llmctl jobs wait %s\n", bifs.Arg(0), bj.Job, bj.Job)
		if *biwait {
			return waitJob(bj.Job, false)
		}
		return nil
	case "uninstall":
		bufs := flag.NewFlagSet("backends uninstall", flag.ExitOnError)
		bujson := bufs.Bool("json", false, "print the raw response JSON (F566)")
		bufs.Parse(reorderFlags(rest))
		if bufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backends uninstall <id> [-json]")
		}
		code, out, err := cc.Post("/api/v1/backends/"+url.PathEscape(bufs.Arg(0))+"/uninstall", nil)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("uninstall returned %d: %s", code, out)
		}
		if *bujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("backend %s uninstalled\n", bufs.Arg(0))
		return nil
	case "add":
		bafs := flag.NewFlagSet("backends add", flag.ExitOnError)
		bajson := bafs.Bool("json", false, "print the raw created-backend JSON (F566)")
		bafs.Parse(reorderFlags(rest))
		if bafs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backends add <plugin.json> [-json]")
		}
		b, err := os.ReadFile(bafs.Arg(0))
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
		if code != 201 {
			return fmt.Errorf("add returned %d: %s", code, out)
		}
		if *bajson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("backend %v added\n", m["id"])
		return nil
	case "remove":
		brfs := flag.NewFlagSet("backends remove", flag.ExitOnError)
		brjson := brfs.Bool("json", false, "print the raw response JSON (F566)")
		brfs.Parse(reorderFlags(rest))
		if brfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backends remove <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/backends/" + url.PathEscape(brfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("remove returned %d: %s", code, out)
		}
		if *brjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("backend %s removed\n", brfs.Arg(0))
		return nil
	default:
		return fmt.Errorf("unknown backends subcommand: %s", sub)
	}
}

func cmdModels(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl models <list|stats|show|add|rename|note|tag|retag|probe|price|ctx|idle|metadata|install|remove|routable|unused|aliases|hf-search|hf-files>")
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
		rout := rfs.String("out", "", "write the raw /v1/models JSON to this file instead of stdout (F499)")
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
		if *rout != "" { // F499: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*rout, body, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *rout, len(body))
			return nil
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
		rnfs := flag.NewFlagSet("models rename", flag.ExitOnError)
		rnjson := rnfs.Bool("json", false, "print the raw response JSON (F523)")
		rnfs.Parse(reorderFlags(rest))
		if rnfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl models rename <id> <new-id> [-json]")
		}
		code, out, err := cc.Do("PATCH", "/api/v1/models/"+url.PathEscape(rnfs.Arg(0))+"/rename",
			map[string]any{"newId": rnfs.Arg(1)})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models rename: %s", out)
		}
		if *rnjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("model %s renamed to %s\n", rnfs.Arg(0), rnfs.Arg(1))
		return nil
	case "note":
		// Free-text annotation on a model, twin of `llmctl note` for
		// instances (F661); empty text clears it.
		mnfs := flag.NewFlagSet("models note", flag.ExitOnError)
		mnjson := mnfs.Bool("json", false, "print the raw response JSON")
		mnfs.Parse(reorderFlags(rest))
		if mnfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl models note <id> [text…] [-json]  (empty text clears)")
		}
		note := strings.Join(mnfs.Args()[1:], " ")
		code, out, err := cc.Do("PATCH", "/api/v1/models/"+url.PathEscape(mnfs.Arg(0))+"/note",
			map[string]any{"note": note})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models note: %s", out)
		}
		if *mnjson {
			fmt.Print(string(out))
			return nil
		}
		if strings.TrimSpace(note) == "" {
			fmt.Printf("model %s note cleared\n", mnfs.Arg(0))
		} else {
			fmt.Printf("model %s note set\n", mnfs.Arg(0))
		}
		return nil
	case "tag":
		// Replace a model's tags (comma-separated); empty clears (F662).
		tgfs := flag.NewFlagSet("models tag", flag.ExitOnError)
		tgjson := tgfs.Bool("json", false, "print the raw response JSON")
		tgfs.Parse(reorderFlags(rest))
		if tgfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl models tag <id> [a,b,c] [-json]  (empty clears)")
		}
		var tags []string
		if tgfs.NArg() > 1 {
			for _, t := range strings.Split(tgfs.Arg(1), ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
		code, out, err := cc.Do("PATCH", "/api/v1/models/"+url.PathEscape(tgfs.Arg(0))+"/tags",
			map[string]any{"tags": tags})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models tag: %s", out)
		}
		if *tgjson {
			fmt.Print(string(out))
			return nil
		}
		if len(tags) == 0 {
			fmt.Printf("model %s tags cleared\n", tgfs.Arg(0))
		} else {
			fmt.Printf("model %s tags: %s\n", tgfs.Arg(0), strings.Join(tags, ", "))
		}
		return nil
	case "retag":
		// Bulk-rename or drop a tag across all models (F662), twin of
		// `memory retag`; the tag rules live in the tags endpoint.
		rtfs := flag.NewFlagSet("models retag", flag.ExitOnError)
		rtrm := rtfs.Bool("rm", false, "drop the tag instead of renaming it")
		rtjson := rtfs.Bool("json", false, "print the count as JSON")
		rtfs.Parse(reorderFlags(rest))
		if rtfs.NArg() < 1 || (!*rtrm && rtfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl models retag <old-tag> <new-tag> | models retag -rm <tag> [-json]")
		}
		oldTag, newTag := rtfs.Arg(0), rtfs.Arg(1)
		code, out, err := cc.Get("/api/v1/models")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("models returned %d: %s", code, out)
		}
		var ms []struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(out, &ms); err != nil {
			return err
		}
		changed := 0
		for _, m := range ms {
			found := false
			var tags []string
			for _, t := range m.Tags {
				if t != oldTag {
					tags = append(tags, t)
					continue
				}
				found = true
				if !*rtrm {
					tags = append(tags, newTag)
				}
			}
			if !found {
				continue
			}
			code, out, err := cc.Do("PATCH", "/api/v1/models/"+url.PathEscape(m.ID)+"/tags", map[string]any{"tags": tags})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("models retag %s: %s", m.ID, out)
			}
			changed++
		}
		if *rtjson {
			fmt.Printf("{\"changed\":%d}\n", changed)
			return nil
		}
		if *rtrm {
			fmt.Printf("dropped tag %q from %d model(s)\n", oldTag, changed)
		} else {
			fmt.Printf("renamed tag %q to %q on %d model(s)\n", oldTag, newTag, changed)
		}
		return nil
	case "add":
		afs := flag.NewFlagSet("models add", flag.ExitOnError)
		backendID := afs.String("backend", "", "backend id (default: llama.cpp)")
		actx := afs.Int("ctx", 0, "default context window in tokens (optional, F551)")
		ajson := afs.Bool("json", false, "print the raw created-model JSON (F551)")
		aout := afs.String("out", "", "write the raw created-model JSON to this file instead of stdout (F513)")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl models add <id> <file> [-backend ID] [-ctx N] [-json] [-out FILE]")
		}
		body := map[string]any{"id": afs.Arg(0), "path": afs.Arg(1)}
		if *backendID != "" {
			body["backend"] = *backendID
		}
		if *actx > 0 {
			body["ctxSize"] = *actx
		}
		code, out, err := cc.Post("/api/v1/models", body)
		if err != nil {
			return err
		}
		if code != 200 && code != 201 {
			return fmt.Errorf("models add: %s", out)
		}
		if *ajson {
			fmt.Print(string(out))
			return nil
		}
		if *aout != "" { // F513: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*aout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *aout, len(out))
			return nil
		}
		fmt.Printf("model %s registered from %s\n", afs.Arg(0), afs.Arg(1))
		return nil
	case "stats":
		// Roll the model library up per backend — models, running, total
		// size (F627).
		sfs := flag.NewFlagSet("models stats", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the per-backend stats as JSON (for scripts)")
		scsv := sfs.Bool("csv", false, "output the per-backend stats as CSV (F627)")
		sout := sfs.String("out", "", "write the stats JSON to this file instead of stdout (F627)")
		sfs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/models")
		if err != nil {
			return err
		}
		var mods []struct {
			BackendID string `json:"backendId"`
			SizeBytes int64  `json:"sizeBytes"`
			Running   bool   `json:"running"`
		}
		if err := json.Unmarshal(out, &mods); err != nil {
			return err
		}
		type bst struct {
			Backend string `json:"backend"`
			Models  int    `json:"models"`
			Running int    `json:"running"`
			Size    int64  `json:"size_bytes"`
		}
		byBackend := map[string]*bst{}
		for _, m := range mods {
			g := byBackend[m.BackendID]
			if g == nil {
				g = &bst{Backend: m.BackendID}
				byBackend[m.BackendID] = g
			}
			g.Models++
			if m.Running {
				g.Running++
			}
			g.Size += m.SizeBytes
		}
		names := make([]string, 0, len(byBackend))
		for k := range byBackend {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool { return byBackend[names[i]].Size > byBackend[names[j]].Size })
		rows := make([]bst, 0, len(names))
		for _, k := range names {
			rows = append(rows, *byBackend[k])
		}
		if *sout != "" { // raw stats JSON to a file, stdout stays clean
			b, _ := json.Marshal(rows)
			if err := os.WriteFile(*sout, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *sout, len(b))
			return nil
		}
		if *sjson {
			b, _ := json.Marshal(rows)
			fmt.Println(string(b))
			return nil
		}
		if *scsv {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"backend", "models", "running", "size_bytes"})
			for _, r := range rows {
				_ = w.Write([]string{r.Backend, strconv.Itoa(r.Models), strconv.Itoa(r.Running),
					strconv.FormatInt(r.Size, 10)})
			}
			w.Flush()
			return w.Error()
		}
		if len(rows) == 0 {
			fmt.Println("no models downloaded")
			return nil
		}
		fmt.Printf("%-14s %7s %8s %12s\n", "BACKEND", "MODELS", "RUNNING", "SIZE")
		for _, r := range rows {
			fmt.Printf("%-14s %7d %8d %12s\n", r.Backend, r.Models, r.Running, fmtSize(r.Size))
		}
		return nil
	case "unused":
		// Models with no running instance and no stored instance record —
		// cleanup candidates (F280).
		uufs := flag.NewFlagSet("models unused", flag.ExitOnError)
		uucsv := uufs.Bool("csv", false, "output the unused models as CSV (F408)")
		uuout := uufs.String("out", "", "write the unused models as JSON to this file instead of stdout (F512)")
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
		if *uuout != "" { // F512: JSON to a file, stdout stays clean
			type unusedRow struct {
				ID        string `json:"id"`
				SizeBytes int64  `json:"sizeBytes"`
			}
			rows := []unusedRow{}
			for _, m := range unused {
				rows = append(rows, unusedRow{m.ID, m.Size})
			}
			b, err := json.Marshal(rows)
			if err != nil {
				return err
			}
			b = append(b, '\n')
			if err := os.WriteFile(*uuout, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *uuout, len(b))
			return nil
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
		tagF := fs.String("tag", "", "only models carrying this tag (F662)")
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
		if *jsonOut && *tagF == "" {
			fmt.Print(string(out))
			return nil
		}
		var list []struct {
			ID        string   `json:"id"`
			BackendID string   `json:"backendId"`
			SizeBytes int64    `json:"sizeBytes"`
			CtxSize   int      `json:"ctxSize"`
			Running   bool     `json:"running"`
			PriceIn   float64  `json:"priceInPerM"`
			PriceOut  float64  `json:"priceOutPerM"`
			IdleMin   int      `json:"idleUnloadMin"`
			Note      string   `json:"note"`
			Tags      []string `json:"tags"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if *tagF != "" { // F662: filter by tag; -json prints the filtered array
			var filtered []struct {
				ID        string   `json:"id"`
				BackendID string   `json:"backendId"`
				SizeBytes int64    `json:"sizeBytes"`
				CtxSize   int      `json:"ctxSize"`
				Running   bool     `json:"running"`
				PriceIn   float64  `json:"priceInPerM"`
				PriceOut  float64  `json:"priceOutPerM"`
				IdleMin   int      `json:"idleUnloadMin"`
				Note      string   `json:"note"`
				Tags      []string `json:"tags"`
			}
			for _, m := range list {
				for _, t := range m.Tags {
					if t == *tagF {
						filtered = append(filtered, m)
						break
					}
				}
			}
			list = filtered
			if *jsonOut {
				b, _ := json.Marshal(list)
				fmt.Println(string(b))
				return nil
			}
		}
		if len(list) == 0 {
			if *csvOut { // keep the stream parseable (F372)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "backend", "size_bytes", "ctx_size", "price_in_per_m", "price_out_per_m", "idle_unload_min", "running", "note", "tags"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no models downloaded")
			if *tagF != "" {
				fmt.Printf("no models carry tag %q\n", *tagF)
			}
			return nil
		}
		if *csvOut { // F372
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "backend", "size_bytes", "ctx_size", "price_in_per_m", "price_out_per_m", "idle_unload_min", "running", "note", "tags"})
			for _, m := range list {
				_ = w.Write([]string{m.ID, m.BackendID, strconv.FormatInt(m.SizeBytes, 10), strconv.Itoa(m.CtxSize),
					strconv.FormatFloat(m.PriceIn, 'f', -1, 64), strconv.FormatFloat(m.PriceOut, 'f', -1, 64),
					strconv.Itoa(m.IdleMin), strconv.FormatBool(m.Running), m.Note, strings.Join(m.Tags, ";")})
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
		mout := mfs.String("out", "", "write the raw metadata JSON to this file instead of stdout (F500)")
		mfs.Parse(reorderFlags(rest[1:]))
		code, out, err := cc.Get("/api/v1/models/" + url.PathEscape(rest[0]) + "/metadata")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("metadata for %s: %s", rest[0], out)
		}
		if *mout != "" { // F500: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*mout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *mout, len(out))
			return nil
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
		pout := pfs.String("out", "", "write the probe JSON to this file instead of stdout (F501)")
		pfs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/models/probe")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("probe returned %d: %s", code, out)
		}
		if *pout != "" { // F501: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*pout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *pout, len(out))
			return nil
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
		iout := ifs.String("out", "", "write the raw response JSON to this file instead of stdout (F522)")
		ifs.Parse(reorderFlags(rest))
		if ifs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl models idle <id> <minutes|never> [-out FILE]  (never = -1, 0 = global default)")
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
		if *iout != "" { // F522: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*iout, outb, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *iout, len(outb))
			return nil
		}
		fmt.Printf("idleUnloadMin %d set for %s\n", n, ifs.Arg(0))
		return nil
	case "aliases":
		fs := flag.NewFlagSet("aliases", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw aliases JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the alias list as CSV (F385)")
		outFile := fs.String("out", "", "write the raw aliases JSON to this file instead of stdout (F498)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/aliases")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("aliases returned %d: %s", code, out)
		}
		if *outFile != "" { // F498: raw JSON to a file, stdout stays clean
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
		afs := flag.NewFlagSet("models alias", flag.ExitOnError)
		ajson := afs.Bool("json", false, "print the raw response JSON (F519)")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl models alias <name> <model-id> [-json]")
		}
		code, out, err := cc.Post("/api/v1/aliases", map[string]string{"alias": afs.Arg(0), "model": afs.Arg(1)})
		if err != nil {
			return err
		}
		if code != 200 && code != 201 {
			return fmt.Errorf("alias returned %d: %s", code, out)
		}
		if *ajson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("alias %s -> %s\n", afs.Arg(0), afs.Arg(1))
		return nil
	case "unalias":
		ufs := flag.NewFlagSet("models unalias", flag.ExitOnError)
		ujson := ufs.Bool("json", false, "print the raw response JSON (F519)")
		ufs.Parse(reorderFlags(rest))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models unalias <name> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/aliases/" + url.PathEscape(ufs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("unalias returned %d: %s", code, out)
		}
		if *ujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("removed alias %s\n", ufs.Arg(0))
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
			Note          string   `json:"note"`
			Tags          []string `json:"tags"`
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
		if mv.Note != "" {
			fmt.Printf("note:     %s\n", mv.Note)
		}
		if len(mv.Tags) > 0 {
			fmt.Printf("tags:     %s\n", strings.Join(mv.Tags, ", "))
		}
		if len(mv.Aliases) > 0 {
			fmt.Printf("aliases:  %s\n", strings.Join(mv.Aliases, ", "))
		}
		if len(mv.Instances) > 0 {
			fmt.Printf("instances: %s\n", strings.Join(mv.Instances, ", "))
		}
		return nil
	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		file := fs.String("file", "", "filename in repo")
		rev := fs.String("rev", "", "HF revision")
		backend := fs.String("backend", "", "backend id (default llama.cpp)")
		id := fs.String("id", "", "model id")
		ijson := fs.Bool("json", false, "print the raw job JSON (for scripts) (F562)")
		iout := fs.String("out", "", "write the raw job JSON to this file instead of stdout (F562)")
		iwait := fs.Bool("wait", false, "block until the download finishes (F576)")
		fs.Parse(reorderFlags(rest))
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models install <repo-or-url> [--file f] [--rev r] [--backend id] [--id ID] [-json] [-out FILE] [--wait]")
		}
		body := map[string]any{"repo": fs.Arg(0), "file": *file, "revision": *rev, "backend": *backend, "id": *id}
		code, out, err := cc.Post("/api/v1/models/install", body)
		if err != nil {
			return err
		}
		if code != 202 {
			return fmt.Errorf("install returned %d: %s", code, out)
		}
		var jr struct {
			Job   string `json:"job"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(out, &jr); err != nil {
			return err
		}
		if *iout != "" { // raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*iout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *iout, len(out))
			if *iwait {
				return waitJob(jr.Job, *ijson)
			}
			return nil
		}
		if *ijson {
			fmt.Print(string(out))
			if *iwait {
				return waitJob(jr.Job, true)
			}
			return nil
		}
		fmt.Printf("download started for model %s (job %s) — track: llmctl jobs wait %s\n", jr.Model, jr.Job, jr.Job)
		if *iwait {
			return waitJob(jr.Job, false)
		}
		return nil
	case "remove":
		mrfs := flag.NewFlagSet("models remove", flag.ExitOnError)
		mrfile := mrfs.Bool("file", false, "also delete a registered file outside the models dir (F162)")
		mrjson := mrfs.Bool("json", false, "print the raw response JSON (F571)")
		mrfs.Parse(reorderFlags(rest))
		if mrfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models remove <id> [--file] [-json]")
		}
		path := "/api/v1/models/" + url.PathEscape(mrfs.Arg(0))
		if *mrfile {
			path += "?file=true"
		}
		code, out, err := cc.Delete(path)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("remove returned %d: %s", code, out)
		}
		if *mrjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("model %s removed\n", mrfs.Arg(0))
		return nil
	case "ctx":
		cfs := flag.NewFlagSet("models ctx", flag.ExitOnError)
		cjson := cfs.Bool("json", false, "print the raw response JSON (for scripts) (F560)")
		cout := cfs.String("out", "", "write the raw response JSON to this file instead of stdout (F521)")
		cfs.Parse(reorderFlags(rest))
		if cfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models ctx <model-id> <tokens|0> [-json] [-out FILE]  (0 = backend default)")
		}
		id := cfs.Arg(0)
		n := 0
		if cfs.NArg() > 1 {
			v, err := strconv.Atoi(cfs.Arg(1))
			if err != nil {
				return fmt.Errorf("invalid token count: %s", cfs.Arg(1))
			}
			n = v
		}
		code, out, err := cc.Do("PUT", "/api/v1/models/"+urlEscape(id)+"/ctx", map[string]any{"ctxSize": n})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("ctx returned %d: %s", code, out)
		}
		if *cout != "" { // F521: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*cout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *cout, len(out))
			return nil
		}
		if *cjson {
			fmt.Print(string(out))
			return nil
		}
		var cr struct {
			CtxSize         int  `json:"ctxSize"`
			RestartRequired bool `json:"restartRequired"`
		}
		if err := json.Unmarshal(out, &cr); err != nil {
			return err
		}
		if cr.CtxSize == 0 {
			fmt.Printf("ctx override cleared for %s — backend default will be used\n", id)
		} else {
			fmt.Printf("ctx %d saved for %s\n", cr.CtxSize, id)
		}
		if cr.RestartRequired {
			fmt.Println("note: a running instance keeps the old value until restarted")
		}
		return nil
	case "price":
		pfs := flag.NewFlagSet("models price", flag.ExitOnError)
		pjson := pfs.Bool("json", false, "print the raw response JSON (for scripts) (F560)")
		pout := pfs.String("out", "", "write the raw response JSON to this file instead of stdout (F520)")
		pfs.Parse(reorderFlags(rest))
		if pfs.NArg() < 3 {
			return fmt.Errorf("usage: llmctl models price <model-id> <in-per-M> <out-per-M> [-json] [-out FILE]  (USD, 0 = free)")
		}
		inP, err := strconv.ParseFloat(pfs.Arg(1), 64)
		if err != nil {
			return fmt.Errorf("invalid price: %s", pfs.Arg(1))
		}
		outP, err := strconv.ParseFloat(pfs.Arg(2), 64)
		if err != nil {
			return fmt.Errorf("invalid price: %s", pfs.Arg(2))
		}
		code, out, err := cc.Do("PATCH", "/api/v1/models/"+urlEscape(pfs.Arg(0))+"/price",
			map[string]any{"priceInPerM": inP, "priceOutPerM": outP})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("price returned %d: %s", code, out)
		}
		if *pout != "" { // F520: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*pout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *pout, len(out))
			return nil
		}
		if *pjson {
			fmt.Print(string(out))
			return nil
		}
		var pr struct {
			PriceInPerM  float64 `json:"priceInPerM"`
			PriceOutPerM float64 `json:"priceOutPerM"`
		}
		if err := json.Unmarshal(out, &pr); err != nil {
			return err
		}
		fmt.Printf("price for %s: $%.4f/M in, $%.4f/M out\n", pfs.Arg(0), pr.PriceInPerM, pr.PriceOutPerM)
		return nil
	case "hf-search":
		hfs := flag.NewFlagSet("models hf-search", flag.ExitOnError)
		hjson := hfs.Bool("json", false, "print the raw search JSON (for scripts)")
		hout := hfs.String("out", "", "write the raw search JSON to this file instead of stdout (F502)")
		hfs.Parse(reorderFlags(rest))
		if hfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models hf-search <query> [-json] [-out FILE]")
		}
		q := strings.Join(hfs.Args(), " ")
		_, out, err := cc.Get("/api/v1/hf/search?q=" + urlEscape(q))
		if err != nil {
			return err
		}
		if *hout != "" { // F502: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*hout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *hout, len(out))
			return nil
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
		fout := ffs.String("out", "", "write the raw files JSON to this file instead of stdout (F503)")
		ffs.Parse(reorderFlags(rest))
		if ffs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl models hf-files <repo> [-json] [-out FILE]")
		}
		_, out, err := cc.Get("/api/v1/hf/files?repo=" + urlEscape(ffs.Arg(0)))
		if err != nil {
			return err
		}
		if *fout != "" { // F503: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*fout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *fout, len(out))
			return nil
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
	jsonOut := fs.Bool("json", false, "print the raw started-instance JSON (F534)")
	var vars stringList
	fs.Var(&vars, "var", "parameter k=v (repeatable)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl start <model> [--backend id] [--instance name] [--var k=v ...] [-json]")
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
	if *jsonOut {
		fmt.Print(string(out))
		return nil
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
	sjson := fs.Bool("json", false, "print the raw response JSON (F566)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 && !*all {
		return fmt.Errorf("usage: llmctl stop <instance> [--force] | llmctl stop --all [--json]")
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
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		var sa struct {
			Stopped []string `json:"stopped"`
		}
		if err := json.Unmarshal(out, &sa); err != nil {
			return err
		}
		fmt.Printf("stopped %d instance(s)\n", len(sa.Stopped))
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
	if code != 200 {
		return fmt.Errorf("stop returned %d: %s", code, out)
	}
	if *sjson {
		fmt.Print(string(out))
		return nil
	}
	fmt.Printf("instance %s stopped\n", fs.Arg(0))
	return nil
}

func cmdRestart(args []string) error {
	fs := flag.NewFlagSet("restart", flag.ExitOnError)
	all := fs.Bool("all", false, "restart every running instance (F445)")
	jsonOut := fs.Bool("json", false, "print the raw response JSON (single) or one aggregate object (--all) (F533, F567)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() == 0 && !*all {
		return fmt.Errorf("usage: llmctl restart <instance> | llmctl restart --all [--json]")
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
		type restartRow struct {
			ID    string `json:"id"`
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		var rows []restartRow
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
				if !*jsonOut {
					fmt.Printf("restart %s failed: %s\n", inst.ID, strings.TrimSpace(string(out)))
				}
				rows = append(rows, restartRow{ID: inst.ID, OK: false, Error: strings.TrimSpace(string(out))})
				continue
			}
			rows = append(rows, restartRow{ID: inst.ID, OK: true})
			if !*jsonOut {
				fmt.Printf("restarted %s\n", inst.ID)
			}
		}
		if *jsonOut { // F533: one aggregate object for scripts
			b, err := json.Marshal(map[string]any{"restarted": rows})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
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
	if code != 200 {
		return fmt.Errorf("restart returned %d: %s", code, out)
	}
	if *jsonOut {
		fmt.Print(string(out))
		return nil
	}
	fmt.Printf("restarted %s\n", fs.Arg(0))
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
	jsonOut := fs.Bool("json", false, "with -daemon: print the raw {size,logs} JSON (F517)")
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
		if *jsonOut { // F517: raw {size,logs} JSON for scripts
			b, err := json.Marshal(map[string]any{"size": size, "logs": logs})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		if *outFile != "" { // F516: write the tail to a file, stdout stays clean
			if err := os.WriteFile(*outFile, []byte(logs), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(logs))
			return nil
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
		return fmt.Errorf("usage: llmctl logs <instance> [--stream stderr|stdout] [--tail N] [-out FILE] [-f] | llmctl logs -daemon [--tail N] [-out FILE] [-f]")
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
		return fmt.Errorf("usage: llmctl scheduler <list|show|add|duplicate|update|run|enable|disable|delete|export|import> [id] [flags]")
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
		sifs := flag.NewFlagSet("scheduler import", flag.ExitOnError)
		sijson := sifs.Bool("json", false, "print {\"created\",\"skipped\"} as JSON (F542)")
		sifs.Parse(reorderFlags(rest))
		if sifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl scheduler import <file.json> [-json]")
		}
		data, err := os.ReadFile(sifs.Arg(0))
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
				if *sijson {
					fmt.Fprintf(os.Stderr, "  skip: %s\n", out)
				} else {
					fmt.Printf("  skip: %s\n", out)
				}
				skipped++
				continue
			}
			created++
		}
		if *sijson {
			b, _ := json.Marshal(map[string]int{"created": created, "skipped": skipped})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("imported %d schedules (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("scheduler list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw scheduler JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the schedule table as CSV (F376)")
		outFile := fs.String("out", "", "write the raw scheduler JSON to this file instead of stdout (F495)")
		tagF := fs.String("tag", "", "only tasks carrying this tag (F681)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/scheduler")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler list returned %d: %s", code, out)
		}
		if *tagF != "" { // F681: exact tag filter on the raw JSON so -json keeps every field
			var raw []map[string]any
			if err := json.Unmarshal(out, &raw); err != nil {
				return err
			}
			keep := raw[:0]
			for _, r := range raw {
				spec, _ := r["spec"].(map[string]any)
				ts, _ := spec["tags"].([]any)
				for _, t := range ts {
					if s, _ := t.(string); s == *tagF {
						keep = append(keep, r)
						break
					}
				}
			}
			if out, err = json.Marshal(keep); err != nil {
				return err
			}
		}
		if *outFile != "" { // F495: raw JSON to a file, stdout stays clean
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
		var recs []struct {
			ID   string `json:"id"`
			Spec struct {
				Name     string   `json:"name"`
				Kind     string   `json:"kind"`
				CronExpr string   `json:"cron_expr"`
				Model    string   `json:"model"`
				Enabled  bool     `json:"enabled"`
				Tags     []string `json:"tags"` // free-form labels (F681)
				NextRun  string   `json:"next_run"`
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
				_ = w.Write([]string{"id", "name", "kind", "cron", "model", "enabled", "next_run", "last_run_time", "last_run_status", "tags"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no scheduled tasks")
			return nil
		}
		if *csvOut { // F376
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "kind", "cron", "model", "enabled", "next_run", "last_run_time", "last_run_status", "tags"})
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
					strconv.FormatBool(r.Spec.Enabled), r.Spec.NextRun, lt, ls, strings.Join(r.Spec.Tags, ";")})
			}
			w.Flush()
			return w.Error()
		}
		fmt.Printf("%-16s %-4s %-12s %-12s %-12s %-10s %-8s %-16s %s\n", "ID", "ON", "KIND", "CRON", "MODEL", "LAST", "NEXT", "TAGS", "NAME")
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
			fmt.Printf("%-16s %-4s %-12s %-12s %-12s %-10s %-8s %-16s %s\n", r.ID, on, kind, r.Spec.CronExpr, r.Spec.Model, last, next, strings.Join(r.Spec.Tags, ","), r.Spec.Name)
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
		kb := fs.String("kb", "", "knowledge base id(s), comma-separated: top matching chunks are injected as context (chat kind, F666)")
		ajson := fs.Bool("json", false, "print the raw created-task JSON (F563)")
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
		if code != 201 {
			return fmt.Errorf("scheduler add returned %d: %s", code, out)
		}
		if *ajson {
			fmt.Print(string(out))
			return nil
		}
		var rec struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(out, &rec); err != nil {
			return err
		}
		fmt.Printf("task %s (%s) added — cron %q\n", *name, rec.ID, *cron)
		return nil
	case "duplicate":
		// Copy an existing schedule under a new name (F646).
		dfs := flag.NewFlagSet("scheduler duplicate", flag.ExitOnError)
		dname := dfs.String("name", "", "name for the copy (default: <old name> copy)")
		djson := dfs.Bool("json", false, "print the raw created-task JSON")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl scheduler duplicate <id> [-name N] [-json]")
		}
		code, out, err := cc.Get("/api/v1/scheduler/" + url.PathEscape(dfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler %s: %s", dfs.Arg(0), out)
		}
		var rec struct {
			Spec struct {
				Name       string `json:"name"`
				CronExpr   string `json:"cron_expr"`
				Kind       string `json:"kind"`
				Model      string `json:"model"`
				Prompt     string `json:"prompt"`
				Instance   string `json:"instance"`
				Keep       int    `json:"keep"`
				KB         string `json:"kb"`
				Enabled    bool   `json:"enabled"`
				WebhookURL string `json:"webhook_url"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(out, &rec); err != nil {
			return err
		}
		name := *dname
		if name == "" {
			name = rec.Spec.Name + " copy"
		}
		body := map[string]any{
			"name": name, "cron_expr": rec.Spec.CronExpr, "kind": rec.Spec.Kind,
			"model": rec.Spec.Model, "prompt": rec.Spec.Prompt,
			"instance": rec.Spec.Instance, "enabled": rec.Spec.Enabled,
		}
		if rec.Spec.Keep > 0 {
			body["keep"] = rec.Spec.Keep
		}
		if rec.Spec.KB != "" {
			body["kb"] = rec.Spec.KB
		}
		if rec.Spec.WebhookURL != "" {
			body["webhook_url"] = rec.Spec.WebhookURL
		}
		code, out, err = cc.Post("/api/v1/scheduler", body)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("scheduler duplicate returned %d: %s", code, out)
		}
		if *djson {
			fmt.Print(string(out))
			return nil
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(out, &created); err != nil {
			return err
		}
		fmt.Printf("task %s (%s) duplicated from %s — cron %q\n", name, created.ID, dfs.Arg(0), rec.Spec.CronExpr)
		return nil
	case "show":
		sfs := flag.NewFlagSet("scheduler show", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw task JSON (for scripts)")
		sout := sfs.String("out", "", "write the raw task JSON to this file instead of stdout (F504)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl scheduler show <id> [-json] [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/scheduler/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler %s: %s", sfs.Arg(0), out)
		}
		if *sout != "" { // F504: raw JSON to a file, stdout stays clean
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
		kb := ufs.String("kb", "", "knowledge base id(s), comma-separated (chat kind, F666)")
		sujson := ufs.Bool("json", false, "print the raw response JSON (F540)")
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
		if *sujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("scheduler task %s updated\n", id)
		return nil
	case "runs":
		fs := flag.NewFlagSet("scheduler runs", flag.ExitOnError)
		n := fs.Int("n", 20, "number of recent runs")
		jsonOut := fs.Bool("json", false, "print the run log as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the run log as CSV (F418)")
		outFile := fs.String("out", "", "write the run log JSON to this file instead of stdout (F476)")
		watch := fs.Int("watch", 0, "poll every N seconds and print new runs until Ctrl-C (0 = print once) (F555)")
		stats := fs.Bool("stats", false, "aggregate the runs per task instead of listing them (F601)")
		taskF := fs.String("task", "", "only runs of this task (id or name) (F688)")
		fs.Parse(reorderFlags(rest))
		type runRow struct {
			Time       time.Time `json:"time"`
			TaskID     string    `json:"taskId"`
			Name       string    `json:"name"`
			Kind       string    `json:"kind"`
			Status     string    `json:"status"`
			DurationMs int64     `json:"durationMs"`
			Detail     string    `json:"detail"`
		}
		fetch := func() ([]runRow, error) {
			code, out, err := cc.Get("/api/v1/scheduler/runs")
			if err != nil {
				return nil, err
			}
			if code != 200 {
				return nil, fmt.Errorf("scheduler runs returned %d: %s", code, out)
			}
			var runs []runRow
			if err := json.Unmarshal(out, &runs); err != nil {
				return nil, err
			}
			if *taskF != "" { // F688: exact task id or name filter
				keep := runs[:0]
				for _, r := range runs {
					if r.TaskID == *taskF || r.Name == *taskF {
						keep = append(keep, r)
					}
				}
				runs = keep
			}
			return runs, nil
		}
		runs, err := fetch()
		if err != nil {
			return err
		}
		if *stats { // F601: one row per task (count, ok/error, avg ms, last run)
			type rstat struct {
				Name     string    `json:"name"`
				Kind     string    `json:"kind"`
				Count    int       `json:"count"`
				OK       int       `json:"ok"`
				Errors   int       `json:"errors"`
				AvgMs    int64     `json:"avgMs"`
				Last     time.Time `json:"last"`
				LastStat string    `json:"lastStatus"`
			}
			byName := map[string]*rstat{}
			sums := map[string]int64{}
			for _, r := range runs {
				g := byName[r.Name]
				if g == nil {
					g = &rstat{Name: r.Name, Kind: r.Kind}
					byName[r.Name] = g
				}
				g.Count++
				sums[r.Name] += r.DurationMs
				if r.Status == "ok" {
					g.OK++
				} else {
					g.Errors++
				}
				if r.Time.After(g.Last) {
					g.Last, g.LastStat = r.Time, r.Status
				}
			}
			names := make([]string, 0, len(byName))
			for k := range byName {
				names = append(names, k)
			}
			sort.Slice(names, func(i, j int) bool { return byName[names[i]].Last.After(byName[names[j]].Last) })
			rows := make([]rstat, 0, len(names))
			for _, k := range names {
				g := byName[k]
				g.AvgMs = sums[k] / int64(g.Count)
				rows = append(rows, *g)
			}
			if *jsonOut || *outFile != "" {
				b, _ := json.Marshal(rows)
				if *outFile != "" {
					if err := os.WriteFile(*outFile, b, 0o644); err != nil {
						return err
					}
					fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
					return nil
				}
				fmt.Println(string(b))
				return nil
			}
			if *csvOut {
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"name", "kind", "count", "ok", "errors", "avg_ms", "last", "last_status"})
				for _, r := range rows {
					_ = w.Write([]string{r.Name, r.Kind, strconv.Itoa(r.Count), strconv.Itoa(r.OK),
						strconv.Itoa(r.Errors), strconv.FormatInt(r.AvgMs, 10),
						r.Last.UTC().Format(time.RFC3339), r.LastStat})
				}
				w.Flush()
				return w.Error()
			}
			if len(rows) == 0 {
				fmt.Println("no scheduler runs yet (ring is in-memory, cleared on daemon restart)")
				return nil
			}
			fmt.Printf("%-20s %-14s %6s %4s %6s %8s %-20s %s\n", "TASK", "KIND", "COUNT", "OK", "ERRORS", "AVG MS", "LAST (UTC)", "LAST STATUS")
			for _, r := range rows {
				fmt.Printf("%-20s %-14s %6d %4d %6d %8d %-20s %s\n", r.Name, r.Kind, r.Count, r.OK, r.Errors,
					r.AvgMs, r.Last.UTC().Format("2006-01-02 15:04:05"), r.LastStat)
			}
			return nil
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
			_ = w.Write([]string{"time", "task_id", "name", "kind", "status", "duration_ms", "detail"})
			for _, r := range runs {
				_ = w.Write([]string{r.Time.UTC().Format(time.RFC3339), r.TaskID, r.Name, r.Kind, r.Status,
					strconv.FormatInt(r.DurationMs, 10), r.Detail})
			}
			w.Flush()
			return w.Error()
		}
		printRow := func(r runRow) {
			detail := r.Detail
			if len(detail) > 60 {
				detail = detail[:60] + "…"
			}
			fmt.Printf("%-19s %-20s %-14s %-6s %8d %s\n", r.Time.Local().Format("2006-01-02 15:04:05"), r.Name, r.Kind, r.Status, r.DurationMs, detail)
		}
		if len(runs) == 0 {
			fmt.Println("no scheduler runs yet (ring is in-memory, cleared on daemon restart)")
		} else {
			if *n > 0 && *n < len(runs) {
				runs = runs[:*n]
			}
			fmt.Printf("%-19s %-20s %-14s %-6s %8s %s\n", "TIME", "TASK", "KIND", "STATUS", "MS", "DETAIL")
			for _, r := range runs {
				printRow(r)
			}
		}
		if *watch <= 0 {
			return nil
		}
		// Live tail: remember what we've shown and print only newer runs (F555).
		seen := map[string]bool{}
		key := func(r runRow) string {
			return fmt.Sprintf("%d|%s|%s", r.Time.UnixNano(), r.Name, r.Kind)
		}
		for _, r := range runs {
			seen[key(r)] = true
		}
		fmt.Printf("\nwatching for new scheduler runs every %ds (Ctrl-C to stop)…\n", *watch)
		for {
			time.Sleep(time.Duration(*watch) * time.Second)
			cur, err := fetch()
			if err != nil {
				return err
			}
			var fresh []runRow
			for _, r := range cur { // newest-first; collect unseen
				if !seen[key(r)] {
					fresh = append(fresh, r)
					seen[key(r)] = true
				}
			}
			for i := len(fresh) - 1; i >= 0; i-- { // print oldest-first
				printRow(fresh[i])
			}
		}
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
	case "tag":
		// Replace a task's tags (comma-separated); empty clears (F681).
		tgfs := flag.NewFlagSet("scheduler tag", flag.ExitOnError)
		tgjson := tgfs.Bool("json", false, "print the raw response JSON")
		tgfs.Parse(reorderFlags(rest))
		if tgfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl scheduler tag <id> [a,b,c] [-json]  (empty clears)")
		}
		var tags []string
		if tgfs.NArg() > 1 {
			for _, t := range strings.Split(tgfs.Arg(1), ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
		code, out, err := cc.Do("PATCH", "/api/v1/scheduler/"+url.PathEscape(tgfs.Arg(0))+"/tags",
			map[string]any{"tags": tags})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler tag returned %d: %s", code, out)
		}
		if *tgjson {
			fmt.Print(string(out))
			return nil
		}
		if len(tags) == 0 {
			fmt.Printf("task %s tags cleared\n", tgfs.Arg(0))
		} else {
			fmt.Printf("task %s tags: %s\n", tgfs.Arg(0), strings.Join(tags, ", "))
		}
		return nil
	case "retag":
		// Bulk-rename or drop a tag across all tasks (F681), twin of `models retag`.
		rtfs := flag.NewFlagSet("scheduler retag", flag.ExitOnError)
		rtrm := rtfs.Bool("rm", false, "drop the tag instead of renaming it")
		rtjson := rtfs.Bool("json", false, "print the count as JSON")
		rtfs.Parse(reorderFlags(rest))
		if rtfs.NArg() < 1 || (!*rtrm && rtfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl scheduler retag <old-tag> <new-tag> | scheduler retag -rm <tag> [-json]")
		}
		oldTag, newTag := rtfs.Arg(0), rtfs.Arg(1)
		code, out, err := cc.Get("/api/v1/scheduler")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("scheduler returned %d: %s", code, out)
		}
		var ts []struct {
			ID   string `json:"id"`
			Spec struct {
				Tags []string `json:"tags"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(out, &ts); err != nil {
			return err
		}
		changed := 0
		for _, t := range ts {
			found := false
			var tags []string
			for _, tag := range t.Spec.Tags {
				if tag != oldTag {
					tags = append(tags, tag)
					continue
				}
				found = true
				if !*rtrm {
					tags = append(tags, newTag)
				}
			}
			if !found {
				continue
			}
			code, out, err := cc.Do("PATCH", "/api/v1/scheduler/"+url.PathEscape(t.ID)+"/tags",
				map[string]any{"tags": tags})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("scheduler retag on %s: %s", t.ID, out)
			}
			changed++
		}
		if *rtjson {
			fmt.Printf("{\"changed\": %d}\n", changed)
			return nil
		}
		if *rtrm {
			fmt.Printf("removed tag %q from %d task(s)\n", oldTag, changed)
		} else {
			fmt.Printf("renamed tag %q -> %q on %d task(s)\n", oldTag, newTag, changed)
		}
		return nil
	case "delete":
		sdfs := flag.NewFlagSet("scheduler delete", flag.ExitOnError)
		sdjson := sdfs.Bool("json", false, "print the raw response JSON (F571)")
		sdfs.Parse(reorderFlags(rest))
		if sdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl scheduler delete <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/scheduler/" + url.PathEscape(sdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		if *sdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("task %s deleted\n", sdfs.Arg(0))
		return nil
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
	by := fs.String("by", "", "with -stats: group by model, key, instance, day, hour or status")
	jsonOut := fs.Bool("json", false, "print the raw requests JSON (for scripts)")
	outFile := fs.String("out", "", "write the raw requests JSON to this file instead of stdout (F473)")
	fs.Parse(reorderFlags(args))
	if *by != "" && *by != "model" && *by != "key" && *by != "instance" && *by != "day" && *by != "hour" && *by != "status" {
		return fmt.Errorf("-by must be model, key, instance, day, hour or status")
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
				case "day": // UTC calendar day, matching the request log (F548)
					g = e.Time.UTC().Format("2006-01-02")
				case "hour": // UTC hour bucket, for same-day analysis (F582)
					g = e.Time.UTC().Format("2006-01-02 15:00")
				case "status": // one row per status code, for error triage (F553)
					g = strconv.Itoa(e.Status)
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
// backupsList prints the snapshot list; shared by the plain call and
// `backups -watch N` (F609).
func backupsList(cc *daemon.ControlClient, args []string) error {
	fs := flag.NewFlagSet("backups list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print the raw backups JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the backup list as CSV (F383)")
	outFile := fs.String("out", "", "write the raw backups JSON to this file instead of stdout (F475)")
	fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once) (F609)")
	fs.Parse(reorderFlags(args))
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
}

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
		// -watch N reprints the snapshot list every N seconds until Ctrl-C
		// (F609), handy while scheduled backups fire.
		watch := 0
		for i, a := range rest {
			if a == "-watch" && i+1 < len(rest) {
				watch, _ = strconv.Atoi(rest[i+1])
			} else if strings.HasPrefix(a, "-watch=") {
				watch, _ = strconv.Atoi(strings.TrimPrefix(a, "-watch="))
			}
		}
		if watch > 0 {
			for {
				fmt.Print("\x1b[2J\x1b[H") // clear screen
				fmt.Printf("live snapshots — refreshing every %ds (Ctrl-C to stop)\n\n", watch)
				if err := backupsList(cc, rest); err != nil {
					return err
				}
				time.Sleep(time.Duration(watch) * time.Second)
			}
		}
		return backupsList(cc, rest)
	case "create":
		cfs := flag.NewFlagSet("backups create", flag.ExitOnError)
		label := cfs.String("name", "", "label embedded in the backup file name (F443)")
		cjson := cfs.Bool("json", false, "print the raw created-backup JSON (F564)")
		cfs.Parse(reorderFlags(rest))
		var body any
		if *label != "" {
			body = map[string]string{"label": *label}
		}
		code, out, err := cc.Post("/api/v1/backups", body)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("backup create returned %d: %s", code, out)
		}
		if *cjson {
			fmt.Print(string(out))
			return nil
		}
		var bv struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		if err := json.Unmarshal(out, &bv); err != nil {
			return err
		}
		fmt.Printf("backup %s written (%s)\n", bv.Name, fmtSize(bv.Size))
		return nil
	case "restore":
		rrfs := flag.NewFlagSet("backups restore", flag.ExitOnError)
		rrjson := rrfs.Bool("json", false, "print the raw response JSON (F546)")
		rrfs.Parse(reorderFlags(rest))
		if rrfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backups restore <name> [-json]")
		}
		code, out, err := cc.Post("/api/v1/backups/"+url.PathEscape(rrfs.Arg(0))+"/restore", nil)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("restore returned %d: %s", code, out)
		}
		if *rrjson { // stdout stays parseable (F546)
			fmt.Fprintln(os.Stderr, "NOTE: restart the daemon (llmctl down && llmctl up) to load the restored state")
			fmt.Print(string(out))
			return nil
		}
		var rr struct {
			Files int `json:"files"`
		}
		if err := json.Unmarshal(out, &rr); err != nil {
			return err
		}
		fmt.Printf("restored %s — %d file(s) imported\n", rrfs.Arg(0), rr.Files)
		fmt.Println("NOTE: restart the daemon (llmctl down && llmctl up) to load the restored state")
		return nil
	case "download":
		dfs := flag.NewFlagSet("backups download", flag.ExitOnError)
		outFile := dfs.String("out", "", "write to this file (default: the backup name)")
		djson := dfs.Bool("json", false, "print {file,bytes} as JSON (F546)")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backups download <name> [-out file] [-json]")
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
		if *djson {
			b, _ := json.Marshal(map[string]any{"file": dest, "bytes": len(body)})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("downloaded %s to %s (%s)\n", name, dest, fmtSize(int64(len(body))))
		return nil
	case "delete":
		bdfs := flag.NewFlagSet("backups delete", flag.ExitOnError)
		bdjson := bdfs.Bool("json", false, "print the raw response JSON (F546)")
		bdfs.Parse(reorderFlags(rest))
		if bdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backups delete <name> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/backups/" + url.PathEscape(bdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backup delete returned %d: %s", code, out)
		}
		if *bdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("backup %s deleted\n", bdfs.Arg(0))
		return nil
	case "verify":
		vfs := flag.NewFlagSet("backups verify", flag.ExitOnError)
		vjson := vfs.Bool("json", false, "print the raw verify-result JSON (F540)")
		vall := vfs.Bool("all", false, "verify every stored snapshot (F561)")
		vfs.Parse(reorderFlags(rest))
		verifyOne := func(name string) (map[string]any, error) {
			code, out, err := cc.Post("/api/v1/backups/"+url.PathEscape(name)+"/verify", map[string]any{})
			if err != nil {
				return nil, err
			}
			if code != 200 {
				return nil, fmt.Errorf("backup verify returned %d: %s", code, out)
			}
			var r map[string]any
			if err := json.Unmarshal(out, &r); err != nil {
				return nil, err
			}
			r["name"] = name
			return r, nil
		}
		if *vall {
			// Verify the whole set; non-zero exit if any snapshot is bad (F561).
			code, out, err := cc.Get("/api/v1/backups")
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("backups list returned %d: %s", code, out)
			}
			var list []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(out, &list); err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("no snapshots to verify")
				return nil
			}
			results := []map[string]any{}
			bad := 0
			for _, b := range list {
				r, err := verifyOne(b.Name)
				if err != nil {
					return err
				}
				results = append(results, r)
				if *vjson {
					continue
				}
				if okv, _ := r["ok"].(bool); okv {
					fmt.Printf("[ok]   %s — %v entries, state.json valid\n", b.Name, r["entries"])
				} else {
					bad++
					fmt.Printf("[fail] %s — %v\n", b.Name, r["error"])
				}
			}
			if *vjson {
				b, err := json.Marshal(results)
				if err != nil {
					return err
				}
				fmt.Println(string(b))
			} else {
				fmt.Printf("%d/%d snapshots ok\n", len(results)-bad, len(results))
			}
			if bad > 0 {
				return fmt.Errorf("%d snapshot(s) failed verification", bad)
			}
			return nil
		}
		if vfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backups verify <name> [-json] | backups verify -all [-json]")
		}
		code, out, err := cc.Post("/api/v1/backups/"+url.PathEscape(vfs.Arg(0))+"/verify", map[string]any{})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backup verify returned %d: %s", code, out)
		}
		if *vjson {
			fmt.Print(string(out))
			return nil
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
			return fmt.Errorf("backup %s: %s", vfs.Arg(0), r.Error)
		}
		fmt.Printf("backup %s ok — %d entries, state.json valid\n", vfs.Arg(0), r.Entries)
		return nil
	case "prune":
		pfs := flag.NewFlagSet("backups prune", flag.ExitOnError)
		keep := pfs.Int("keep", 0, "snapshots to keep (0 = settings.backupKeep or 7)")
		pjson := pfs.Bool("json", false, "print the raw prune-result JSON (F558)")
		pfs.Parse(reorderFlags(rest))
		code, out, err := cc.Post("/api/v1/backups/prune", map[string]any{"keep": *keep})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("backups prune returned %d: %s", code, out)
		}
		if *pjson {
			fmt.Print(string(out))
			return nil
		}
		var r struct {
			Keep    int      `json:"keep"`
			Deleted []string `json:"deleted"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			return err
		}
		if len(r.Deleted) == 0 {
			fmt.Printf("nothing to prune (keeping newest %d)\n", r.Keep)
			return nil
		}
		fmt.Printf("pruned %d snapshot(s), keeping newest %d:\n", len(r.Deleted), r.Keep)
		for _, n := range r.Deleted {
			fmt.Println("  -", n)
		}
		return nil
	default:
		return fmt.Errorf("unknown backups subcommand %q (list|create|restore|verify|prune|delete)", sub)
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
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live metrics — refreshing every %ds (Ctrl-C to stop)\n\n", *watch)
		if err := printOnce(); err != nil { // F620: -json/-out honored each tick
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
// cmdUsage prints daily usage; -watch N reprints the view every N seconds
// until interrupted (F587), like status -watch.
func cmdUsage(args []string) error {
	watch := 0
	for i, a := range args {
		if a == "-watch" && i+1 < len(args) {
			watch, _ = strconv.Atoi(args[i+1])
		} else if strings.HasPrefix(a, "-watch=") {
			watch, _ = strconv.Atoi(strings.TrimPrefix(a, "-watch="))
		}
	}
	if watch <= 0 {
		return usageOnce(args)
	}
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live usage — refreshing every %ds (Ctrl-C to stop)\n\n", watch)
		if err := usageOnce(args); err != nil {
			return err
		}
		time.Sleep(time.Duration(watch) * time.Second)
	}
}

func usageOnce(args []string) error {
	fs := flag.NewFlagSet("usage", flag.ExitOnError)
	n := fs.Int("n", 14, "number of days")
	key := fs.String("key", "", "show per-key daily usage for this key id instead of per-model")
	modelFlag := fs.String("model", "", "in per-model mode, show only this model id")
	keysFlag := fs.Bool("keys", false, "show daily usage for all keys instead of per-model")
	csvOut := fs.Bool("csv", false, "output CSV instead of a table")
	jsonOut := fs.Bool("json", false, "print the raw usage JSON (for scripts)")
	outFile := fs.String("out", "", "write the usage JSON to this file instead of stdout (F474)")
	fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once) (F587)")
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
	stats := fs.Bool("stats", false, "aggregate the events by kind instead of listing them (F599)")
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
	if (*jsonOut || *outFile != "") && !*stats { // -out writes the same payload to a file (F468)
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
	if *stats { // F599: one row per event kind (count, last seen, who)
		type est struct {
			Kind      string    `json:"kind"`
			Count     int       `json:"count"`
			Last      time.Time `json:"last"`
			Instances []string  `json:"instances,omitempty"`
			Models    []string  `json:"models,omitempty"`
		}
		byKind := map[string]*est{}
		insts := map[string]map[string]bool{}
		mods := map[string]map[string]bool{}
		for _, e := range evs {
			g := byKind[e.Kind]
			if g == nil {
				g = &est{Kind: e.Kind}
				byKind[e.Kind] = g
				insts[e.Kind] = map[string]bool{}
				mods[e.Kind] = map[string]bool{}
			}
			g.Count++
			if e.Time.After(g.Last) {
				g.Last = e.Time
			}
			if e.Instance != "" {
				insts[e.Kind][e.Instance] = true
			}
			if e.Model != "" {
				mods[e.Kind][e.Model] = true
			}
		}
		names := make([]string, 0, len(byKind))
		for k := range byKind {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool { return byKind[names[i]].Count > byKind[names[j]].Count })
		rows := make([]est, 0, len(names))
		for _, k := range names {
			g := byKind[k]
			for _, s := range []struct {
				set map[string]bool
				dst *[]string
			}{{insts[k], &g.Instances}, {mods[k], &g.Models}} {
				for v := range s.set {
					*s.dst = append(*s.dst, v)
				}
				sort.Strings(*s.dst)
			}
			rows = append(rows, *g)
		}
		if *jsonOut || *outFile != "" {
			b, _ := json.Marshal(rows)
			if *outFile != "" {
				if err := os.WriteFile(*outFile, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
				return nil
			}
			fmt.Println(string(b))
			return nil
		}
		if *csvOut {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"kind", "count", "last", "instances", "models"})
			for _, r := range rows {
				_ = w.Write([]string{r.Kind, strconv.Itoa(r.Count), r.Last.UTC().Format(time.RFC3339),
					strings.Join(r.Instances, " "), strings.Join(r.Models, " ")})
			}
			w.Flush()
			return w.Error()
		}
		if len(rows) == 0 {
			fmt.Println("no events since daemon start")
			return nil
		}
		fmt.Printf("%-26s %6s %-20s %s\n", "EVENT", "COUNT", "LAST (UTC)", "INSTANCES / MODELS")
		for _, r := range rows {
			who := strings.Join(append(append([]string{}, r.Instances...), r.Models...), " ")
			fmt.Printf("%-26s %6d %-20s %s\n", r.Kind, r.Count, r.Last.UTC().Format("2006-01-02 15:04:05"), who)
		}
		return nil
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
		tfs := flag.NewFlagSet("webhooks test", flag.ExitOnError)
		tjson := tfs.Bool("json", false, "print the raw response JSON (F573)")
		tev := tfs.String("event", "", "event name to send (default \"test\"; F591)")
		tmsg := tfs.String("message", "", "message text to send (F591)")
		tfs.Parse(reorderFlags(args[1:]))
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		body := map[string]any{}
		if *tev != "" {
			body["event"] = *tev
		}
		if *tmsg != "" {
			body["message"] = *tmsg
		}
		code, out, err := cc.Post("/api/v1/settings/notify-test", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("notify-test returned %d: %s", code, out)
		}
		if *tjson {
			fmt.Print(string(out))
			return nil
		}
		var tr struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(out, &tr); err != nil {
			return err
		}
		if !tr.OK {
			return fmt.Errorf("webhook test failed: %s", tr.Error)
		}
		fmt.Println("test notification delivered to the configured webhook")
		return nil
	}
	if len(args) > 0 && args[0] == "retry" { // F679: re-send a logged delivery
		rfs := flag.NewFlagSet("webhooks retry", flag.ExitOnError)
		rjson := rfs.Bool("json", false, "print the raw response JSON")
		rfs.Parse(reorderFlags(args[1:]))
		if rfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl webhooks retry <delivery-id> [-json]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Post("/api/v1/webhook/retry", map[string]any{"id": rfs.Arg(0)})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("webhook retry returned %d: %s", code, out)
		}
		if *rjson {
			fmt.Print(string(out))
			return nil
		}
		var rr struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(out, &rr); err != nil {
			return err
		}
		if !rr.OK {
			return fmt.Errorf("webhook retry failed: %s", rr.Error)
		}
		fmt.Printf("delivery %s re-sent\n", rfs.Arg(0))
		return nil
	}
	fs := flag.NewFlagSet("webhooks", flag.ExitOnError)
	n := fs.Int("n", 20, "number of deliveries")
	event := fs.String("event", "", "only deliveries for this event name, e.g. instance.crashed (F357)")
	csvOut := fs.Bool("csv", false, "output the delivery log as CSV (F380)")
	jsonOut := fs.Bool("json", false, "print the delivery log as JSON (for scripts)")
	outFile := fs.String("out", "", "write the delivery log JSON to this file instead of stdout (F478)")
	watch := fs.Int("watch", 0, "poll every N seconds and print new deliveries until Ctrl-C (0 = print once) (F556)")
	stats := fs.Bool("stats", false, "aggregate the deliveries per event name instead of listing them (F624)")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	path := "/api/v1/webhook/log?limit=" + strconv.Itoa(*n)
	if *event != "" {
		path += "&event=" + url.QueryEscape(*event)
	}
	type hookRow struct {
		ID     string    `json:"id"` // delivery id for `webhooks retry` (F679)
		Time   time.Time `json:"time"`
		URL    string    `json:"url"`
		Event  string    `json:"event"`
		Status int       `json:"status"`
		OK     bool      `json:"ok"`
		Error  string    `json:"error"`
	}
	fetch := func() ([]hookRow, error) {
		code, out, err := cc.Get(path)
		if err != nil {
			return nil, err
		}
		if code != 200 {
			return nil, fmt.Errorf("webhook log returned %d", code)
		}
		var list []hookRow
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	list, err := fetch()
	if err != nil {
		return err
	}
	if *stats { // F624: one row per event name (total, ok, failed, last attempt)
		type wst struct {
			Event  string    `json:"event"`
			Total  int       `json:"total"`
			OK     int       `json:"ok"`
			Failed int       `json:"failed"`
			Last   time.Time `json:"last"`
		}
		byEvent := map[string]*wst{}
		for _, d := range list {
			g := byEvent[d.Event]
			if g == nil {
				g = &wst{Event: d.Event}
				byEvent[d.Event] = g
			}
			g.Total++
			if d.OK {
				g.OK++
			} else {
				g.Failed++
			}
			if d.Time.After(g.Last) {
				g.Last = d.Time
			}
		}
		names := make([]string, 0, len(byEvent))
		for k := range byEvent {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool { return byEvent[names[i]].Total > byEvent[names[j]].Total })
		rows := make([]wst, 0, len(names))
		for _, k := range names {
			rows = append(rows, *byEvent[k])
		}
		if *jsonOut || *outFile != "" {
			b, _ := json.Marshal(rows)
			if *outFile != "" {
				if err := os.WriteFile(*outFile, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
				return nil
			}
			fmt.Println(string(b))
			return nil
		}
		if *csvOut {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"event", "total", "ok", "failed", "last"})
			for _, r := range rows {
				_ = w.Write([]string{r.Event, strconv.Itoa(r.Total), strconv.Itoa(r.OK),
					strconv.Itoa(r.Failed), r.Last.UTC().Format(time.RFC3339)})
			}
			w.Flush()
			return w.Error()
		}
		if len(rows) == 0 {
			fmt.Println("no webhook deliveries yet")
			return nil
		}
		fmt.Printf("%-26s %6s %4s %6s %-20s\n", "EVENT", "TOTAL", "OK", "FAILED", "LAST (UTC)")
		for _, r := range rows {
			fmt.Printf("%-26s %6d %4d %6d %-20s\n", r.Event, r.Total, r.OK, r.Failed,
				r.Last.UTC().Format("2006-01-02 15:04:05"))
		}
		return nil
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
	if len(list) == 0 && !*csvOut && *watch <= 0 {
		fmt.Println("no webhook deliveries yet")
		return nil
	}
	if *csvOut { // F380
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "time", "event", "url", "status", "ok", "error"})
		for _, d := range list {
			status := ""
			if d.Status > 0 {
				status = strconv.Itoa(d.Status)
			}
			_ = w.Write([]string{d.ID, d.Time.UTC().Format(time.RFC3339), d.Event, d.URL, status,
				strconv.FormatBool(d.OK), d.Error})
		}
		w.Flush()
		return w.Error()
	}
	printRow := func(d hookRow) {
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
		fmt.Printf("%-6s %-14s %-24s %-6s %-5s %s\n", d.ID,
			d.Time.Local().Format("01-02 15:04:05"), ev, status, ok, info)
	}
	fmt.Printf("%-6s %-14s %-24s %-6s %-5s %s\n", "ID", "TIME", "EVENT", "STATUS", "OK", "URL / ERROR")
	for _, d := range list {
		printRow(d)
	}
	if *watch <= 0 {
		return nil
	}
	// Live tail: remember what we've shown and print only newer deliveries (F556).
	seen := map[string]bool{}
	key := func(d hookRow) string {
		if d.ID != "" { // unique per attempt since F679
			return d.ID
		}
		return fmt.Sprintf("%d|%s|%s|%d", d.Time.UnixNano(), d.Event, d.URL, d.Status)
	}
	for _, d := range list {
		seen[key(d)] = true
	}
	fmt.Printf("\nwatching for new webhook deliveries every %ds (Ctrl-C to stop)…\n", *watch)
	for {
		time.Sleep(time.Duration(*watch) * time.Second)
		cur, err := fetch()
		if err != nil {
			return err
		}
		var fresh []hookRow
		for _, d := range cur { // newest-first; collect unseen
			if !seen[key(d)] {
				fresh = append(fresh, d)
				seen[key(d)] = true
			}
		}
		for i := len(fresh) - 1; i >= 0; i-- { // print oldest-first
			printRow(fresh[i])
		}
	}
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
			ID        string   `json:"id"`
			Title     string   `json:"title"`
			Model     string   `json:"model"`
			Tags      []string `json:"tags"` // free-form labels (F678)
			UpdatedAt string   `json:"updatedAt"`
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
		fmt.Printf("id:      %s\ntitle:   %s\nmodel:   %s\n", ch.ID, ch.Title, ch.Model)
		if len(ch.Tags) > 0 { // tags line only when present (F678)
			fmt.Printf("tags:    %s\n", strings.Join(ch.Tags, ", "))
		}
		fmt.Printf("updated: %s\ntokens:  %d\n\n", ch.UpdatedAt, ch.Context.UsedTokens)
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
		qcsv := qfs.Bool("csv", false, "output the matches as CSV — one row per matched message (F617)")
		qout := qfs.String("out", "", "write the raw search JSON to this file instead of stdout (F482)")
		qfs.Parse(reorderFlags(args[1:]))
		if qfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats search <query> [-json] [-csv] [-out FILE]")
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
		if *qcsv { // F617: one row per matched message
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"chat_id", "title", "model", "role", "content"})
			for _, r := range res {
				for _, m := range r.Matches {
					_ = w.Write([]string{r.ConversationID, r.Title, r.Model, m.Role, m.Content})
				}
			}
			w.Flush()
			return w.Error()
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
		tagAll := xfs.String("tag", "", "only export chats carrying this tag (F686)")
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
			ID    string   `json:"id"`
			Title string   `json:"title"`
			Tags  []string `json:"tags"`
		}
		if err := json.Unmarshal(out, &chats); err != nil {
			return err
		}
		if *tagAll != "" { // F686: same exact-tag filter as `chats list -tag`
			keep := chats[:0]
			for _, c := range chats {
				for _, t := range c.Tags {
					if t == *tagAll {
						keep = append(keep, c)
						break
					}
				}
			}
			chats = keep
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
		outFile := ifs.String("out", "", "write the saved chat JSON to this file instead of stdout (F510)")
		ifs.Parse(reorderFlags(args[1:]))
		if ifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats import <file.json> [-json] [-out FILE]")
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
		if *outFile != "" { // F510: raw JSON to a file, stdout stays clean
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
		fjson := ffs.Bool("json", false, "print the raw new-chat JSON (F544)")
		ffs.Parse(reorderFlags(args[1:]))
		if ffs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl chats fork <id> [-at N] [-title T] [-json]")
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
		if *fjson {
			fmt.Print(string(out))
			return nil
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
		rjson := rfs.Bool("json", false, "print the raw updated-chat JSON (F544)")
		rfs.Parse(reorderFlags(args[1:]))
		if rfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl chats rename <id> <new title> [-json]")
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
		if *rjson {
			fmt.Print(string(out))
			return nil
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
		prjson := pfs.Bool("json", false, "print the prune result as JSON (F544)")
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
			if *prjson {
				b, _ := json.Marshal(map[string]any{"pruned": 0, "doomed": 0, "pinnedSkipped": skippedPinned, "olderThan": cutoff.Format("2006-01-02")})
				fmt.Println(string(b))
				return nil
			}
			fmt.Printf("no chats older than %s (pinned skipped: %d)\n", cutoff.Format("2006-01-02"), skippedPinned)
			return nil
		}
		if !*yes {
			if *prjson {
				rows := []map[string]string{}
				for _, c := range doomed {
					rows = append(rows, map[string]string{"id": c.ID, "title": c.Title, "updatedAt": c.UpdatedAt.Format("2006-01-02")})
				}
				b, _ := json.Marshal(map[string]any{"dryRun": true, "wouldDelete": len(doomed), "pinnedSkipped": skippedPinned, "olderThan": cutoff.Format("2006-01-02"), "chats": rows})
				fmt.Println(string(b))
				return nil
			}
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
		if *prjson {
			b, _ := json.Marshal(map[string]any{"pruned": n, "doomed": len(doomed), "pinnedSkipped": skippedPinned, "olderThan": cutoff.Format("2006-01-02")})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("pruned %d/%d chat(s) (pinned skipped: %d)\n", n, len(doomed), skippedPinned)
		return nil
	}
	if len(args) > 0 && args[0] == "delete-all" {
		dafs := flag.NewFlagSet("chats delete-all", flag.ExitOnError)
		yes := dafs.Bool("yes", false, "confirm deletion of every saved chat")
		dajson := dafs.Bool("json", false, "print {deleted,total} as JSON (F546)")
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
			if *dajson {
				fmt.Println(`{"deleted":0,"total":0}`)
				return nil
			}
			fmt.Println("no saved chats")
			return nil
		}
		n := 0
		for _, c := range list {
			if code, _, err := cc.Delete("/api/v1/chats/" + url.PathEscape(c.ID)); err == nil && code == 200 {
				n++
			}
		}
		if *dajson {
			b, _ := json.Marshal(map[string]int{"deleted": n, "total": len(list)})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("deleted %d/%d saved chats\n", n, len(list))
		return nil
	}
	if len(args) > 0 && (args[0] == "pin" || args[0] == "unpin") {
		pufs := flag.NewFlagSet("chats "+args[0], flag.ExitOnError)
		pujson := pufs.Bool("json", false, "print the raw response JSON (F539)")
		pufs.Parse(reorderFlags(args[1:]))
		if pufs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl chats %s <id> [-json]", args[0])
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Post("/api/v1/chats/"+url.PathEscape(pufs.Arg(0))+"/pin",
			map[string]any{"pinned": args[0] == "pin"})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat %s: %s", args[0], out)
		}
		if *pujson {
			fmt.Print(string(out))
			return nil
		}
		word := "unpinned"
		if args[0] == "pin" {
			word = "pinned"
		}
		fmt.Printf("%s chat %s\n", word, pufs.Arg(0))
		return nil
	}
	if len(args) > 0 && args[0] == "tag" {
		// Replace a chat's tags (comma-separated); empty clears (F676).
		tgfs := flag.NewFlagSet("chats tag", flag.ExitOnError)
		tgjson := tgfs.Bool("json", false, "print the raw response JSON")
		tgfs.Parse(reorderFlags(args[1:]))
		if tgfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl chats tag <id> [a,b,c] [-json]  (empty clears)")
		}
		var tags []string
		if tgfs.NArg() > 1 {
			for _, t := range strings.Split(tgfs.Arg(1), ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Do("PATCH", "/api/v1/chats/"+url.PathEscape(tgfs.Arg(0))+"/tags",
			map[string]any{"tags": tags})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chats tag: %s", out)
		}
		if *tgjson {
			fmt.Print(string(out))
			return nil
		}
		if len(tags) == 0 {
			fmt.Printf("chat %s tags cleared\n", tgfs.Arg(0))
		} else {
			fmt.Printf("chat %s tags: %s\n", tgfs.Arg(0), strings.Join(tags, ", "))
		}
		return nil
	}
	if len(args) > 0 && args[0] == "retag" {
		// Bulk-rename or drop a tag across all chats (F676), twin of
		// `models retag`; the tag rules live in the tags endpoint.
		rtfs := flag.NewFlagSet("chats retag", flag.ExitOnError)
		rtrm := rtfs.Bool("rm", false, "drop the tag instead of renaming it")
		rtjson := rtfs.Bool("json", false, "print the count as JSON")
		rtfs.Parse(reorderFlags(args[1:]))
		if rtfs.NArg() < 1 || (!*rtrm && rtfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl chats retag <old-tag> <new-tag> | chats retag -rm <tag> [-json]")
		}
		oldTag, newTag := rtfs.Arg(0), rtfs.Arg(1)
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
		var cs []struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(out, &cs); err != nil {
			return err
		}
		changed := 0
		for _, c := range cs {
			found := false
			var tags []string
			for _, t := range c.Tags {
				if t != oldTag {
					tags = append(tags, t)
					continue
				}
				found = true
				if !*rtrm {
					tags = append(tags, newTag)
				}
			}
			if !found {
				continue
			}
			code, out, err := cc.Do("PATCH", "/api/v1/chats/"+url.PathEscape(c.ID)+"/tags",
				map[string]any{"tags": tags})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("chats retag on %s: %s", c.ID, out)
			}
			changed++
		}
		if *rtjson {
			fmt.Printf("{\"changed\": %d}\n", changed)
			return nil
		}
		if *rtrm {
			fmt.Printf("removed tag %q from %d chat(s)\n", oldTag, changed)
		} else {
			fmt.Printf("renamed tag %q -> %q on %d chat(s)\n", oldTag, newTag, changed)
		}
		return nil
	}
	if len(args) > 0 && args[0] == "delete" {
		cdfs := flag.NewFlagSet("chats delete", flag.ExitOnError)
		cdjson := cdfs.Bool("json", false, "print the raw response JSON (F538)")
		cdfs.Parse(reorderFlags(args[1:]))
		if cdfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl chats delete <id> [-json]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Delete("/api/v1/chats/" + url.PathEscape(cdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("chat delete: %s", out)
		}
		if *cdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted chat %s\n", cdfs.Arg(0))
		return nil
	}
	fs := flag.NewFlagSet("chats", flag.ExitOnError)
	n := fs.Int("n", 20, "number of conversations")
	jsonOut := fs.Bool("json", false, "print the chat list as JSON (for scripts)")
	csvOut := fs.Bool("csv", false, "output the chat list as CSV (F378)")
	outFile := fs.String("out", "", "write the chat list JSON to this file instead of stdout (F492)")
	stats := fs.Bool("stats", false, "aggregate the chats per model instead of listing them (F625)")
	tagF := fs.String("tag", "", "only chats carrying this tag (F676)")
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
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Model     string   `json:"model"`
		Tags      []string `json:"tags"`
		Messages  int      `json:"messages"`
		Tokens    int      `json:"tokens"` // total token usage (F603)
		UpdatedAt string   `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return err
	}
	if *tagF != "" { // F676: exact tag filter, twin of models list -tag
		keep := list[:0]
		for _, c := range list {
			for _, t := range c.Tags {
				if t == *tagF {
					keep = append(keep, c)
					break
				}
			}
		}
		list = keep
	}
	if filter != "" {
		var keep []struct {
			ID        string   `json:"id"`
			Title     string   `json:"title"`
			Model     string   `json:"model"`
			Tags      []string `json:"tags"`
			Messages  int      `json:"messages"`
			Tokens    int      `json:"tokens"`
			UpdatedAt string   `json:"updatedAt"`
		}
		for _, c := range list {
			if strings.Contains(strings.ToLower(c.Title), filter) || strings.Contains(strings.ToLower(c.Model), filter) {
				keep = append(keep, c)
			}
		}
		list = keep
	}
	if *stats { // F625: one row per model (chats, messages, tokens, last updated)
		type cst struct {
			Model    string `json:"model"`
			Chats    int    `json:"chats"`
			Messages int    `json:"messages"`
			Tokens   int    `json:"tokens"`
			Last     string `json:"last"`
		}
		byModel := map[string]*cst{}
		lastT := map[string]time.Time{}
		for _, c := range list {
			g := byModel[c.Model]
			if g == nil {
				g = &cst{Model: c.Model}
				byModel[c.Model] = g
			}
			g.Chats++
			g.Messages += c.Messages
			g.Tokens += c.Tokens
			if t, err := time.Parse(time.RFC3339, c.UpdatedAt); err == nil {
				if t.After(lastT[c.Model]) {
					lastT[c.Model] = t
				}
			} else if g.Last == "" {
				g.Last = c.UpdatedAt
			}
		}
		names := make([]string, 0, len(byModel))
		for k := range byModel {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool { return byModel[names[i]].Tokens > byModel[names[j]].Tokens })
		rows := make([]cst, 0, len(names))
		for _, k := range names {
			g := byModel[k]
			if !lastT[k].IsZero() {
				g.Last = lastT[k].UTC().Format(time.RFC3339)
			}
			rows = append(rows, *g)
		}
		if *jsonOut || *outFile != "" {
			b, _ := json.Marshal(rows)
			if *outFile != "" {
				if err := os.WriteFile(*outFile, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
				return nil
			}
			fmt.Println(string(b))
			return nil
		}
		if *csvOut {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"model", "chats", "messages", "tokens", "last"})
			for _, r := range rows {
				_ = w.Write([]string{r.Model, strconv.Itoa(r.Chats), strconv.Itoa(r.Messages),
					strconv.Itoa(r.Tokens), r.Last})
			}
			w.Flush()
			return w.Error()
		}
		if len(rows) == 0 {
			fmt.Println("no saved chats")
			return nil
		}
		fmt.Printf("%-18s %6s %6s %9s %-20s\n", "MODEL", "CHATS", "MSGS", "TOKENS", "LAST (UTC)")
		for _, r := range rows {
			last := r.Last
			if t, err := time.Parse(time.RFC3339, r.Last); err == nil {
				last = t.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-18s %6d %6d %9d %-20s\n", r.Model, r.Chats, r.Messages, r.Tokens, last)
		}
		return nil
	}
	if len(list) > *n {
		list = list[:*n]
	}
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F492)
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
		if *csvOut { // keep the stream parseable (F378)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "title", "model", "messages", "updated_at", "tags"})
			w.Flush()
			return w.Error()
		}
		fmt.Println("no saved chats")
		return nil
	}
	if *csvOut { // F378
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"id", "title", "model", "messages", "tokens", "updated_at", "tags"})
		for _, c := range list {
			_ = w.Write([]string{c.ID, c.Title, c.Model, strconv.Itoa(c.Messages), strconv.Itoa(c.Tokens), c.UpdatedAt, strings.Join(c.Tags, ";")})
		}
		w.Flush()
		return w.Error()
	}
	fmt.Printf("%-18s %-14s %-4s %7s %-14s %-48s %s\n", "ID", "MODEL", "MSGS", "TOKENS", "UPDATED", "TITLE", "TAGS")
	for _, c := range list {
		updated := c.UpdatedAt
		if t, err := time.Parse(time.RFC3339, c.UpdatedAt); err == nil {
			updated = t.Local().Format("01-02 15:04")
		}
		title := c.Title
		if len(title) > 48 {
			title = title[:48] + "…"
		}
		fmt.Printf("%-18s %-14s %-4d %7d %-14s %-48s %s\n", c.ID, c.Model, c.Messages, c.Tokens, updated, title, strings.Join(c.Tags, ","))
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
		case "list", "show", "add", "update", "export", "retag", "enable", "disable", "delete":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "add":
		// Create a skill from a JSON manifest file (F288).
		safs := flag.NewFlagSet("skills add", flag.ExitOnError)
		sajson := safs.Bool("json", false, "print the raw created-skill JSON (F540)")
		safs.Parse(reorderFlags(rest))
		if safs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl skills add <manifest.json> [-json]")
		}
		data, err := os.ReadFile(safs.Arg(0))
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
		if *sajson {
			fmt.Print(string(out))
			return nil
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
	case "update":
		// Edit an existing skill's metadata in place (PUT, F652).
		sufs := flag.NewFlagSet("skills update", flag.ExitOnError)
		suname := sufs.String("name", "", "display name")
		sudesc := sufs.String("desc", "", "description")
		sutmpl := sufs.String("template", "", "prompt template")
		sutags := sufs.String("tags", "", "comma-separated tags")
		sujson := sufs.Bool("json", false, "print the raw updated-skill JSON")
		sufs.Parse(reorderFlags(rest))
		if sufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl skills update <id> [-name N] [-desc D] [-template T] [-tags a,b] [-json]")
		}
		code, out, err := cc.Get("/api/v1/skills/" + url.PathEscape(sufs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skills update: %s", out)
		}
		var cur struct {
			Manifest struct {
				ShortName      string   `json:"short_name"`
				Name           string   `json:"name"`
				Description    string   `json:"description"`
				PromptTemplate string   `json:"prompt_template"`
				Tags           []string `json:"tags"`
				Enabled        bool     `json:"enabled"`
			} `json:"manifest"`
		}
		if err := json.Unmarshal(out, &cur); err != nil {
			return err
		}
		body := map[string]any{
			"short_name":      cur.Manifest.ShortName,
			"name":            cur.Manifest.Name,
			"description":     cur.Manifest.Description,
			"prompt_template": cur.Manifest.PromptTemplate,
			"tags":            cur.Manifest.Tags,
			"enabled":         cur.Manifest.Enabled,
		}
		if *suname != "" {
			body["name"] = *suname
		}
		if *sudesc != "" {
			body["description"] = *sudesc
		}
		if *sutmpl != "" {
			body["prompt_template"] = *sutmpl
		}
		if *sutags != "" {
			list := []string{}
			for _, t := range strings.Split(*sutags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					list = append(list, t)
				}
			}
			body["tags"] = list
		}
		code, out, err = cc.Put("/api/v1/skills/"+url.PathEscape(sufs.Arg(0)), body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skills update: %s", out)
		}
		if *sujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("skill %s updated\n", sufs.Arg(0))
		return nil
	case "retag":
		// Bulk-rename or drop a tag across all skills (F663), twin of
		// `memory retag`: fetch the list, PUT each manifest whose tags
		// contain the old one.
		rfs := flag.NewFlagSet("skills retag", flag.ExitOnError)
		rm := rfs.Bool("rm", false, "drop the tag instead of renaming it")
		rjson := rfs.Bool("json", false, "print the count as JSON")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() < 1 || (!*rm && rfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl skills retag <old-tag> <new-tag> | skills retag -rm <tag> [-json]")
		}
		oldTag, newTag := rfs.Arg(0), rfs.Arg(1)
		code, out, err := cc.Get("/api/v1/skills")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skills returned %d: %s", code, out)
		}
		var list []struct {
			Manifest map[string]any `json:"manifest"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		changed := 0
		for _, s := range list {
			m := s.Manifest
			if m == nil {
				continue
			}
			raw, _ := json.Marshal(m["tags"])
			var tags []string
			_ = json.Unmarshal(raw, &tags)
			found := false
			var next []string
			for _, t := range tags {
				if t != oldTag {
					next = append(next, t)
					continue
				}
				found = true
				if !*rm {
					next = append(next, newTag)
				}
			}
			if !found {
				continue
			}
			id, _ := m["id"].(string)
			if id == "" {
				continue
			}
			m["tags"] = next
			code, out, err := cc.Put("/api/v1/skills/"+url.PathEscape(id), m)
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("skills retag %s: %s", id, out)
			}
			changed++
		}
		if *rjson {
			fmt.Printf("{\"changed\":%d}\n", changed)
			return nil
		}
		if *rm {
			fmt.Printf("dropped tag %q from %d skill(s)\n", oldTag, changed)
		} else {
			fmt.Printf("renamed tag %q to %q on %d skill(s)\n", oldTag, newTag, changed)
		}
		return nil
	case "list":
		fs := flag.NewFlagSet("skills", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the skills as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the skill list as CSV (F384)")
		outFile := fs.String("out", "", "write the skills JSON to this file instead of stdout (F489)")
		tagF := fs.String("tag", "", "only skills carrying this tag (F674)")
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
		if *tagF != "" { // F674: exact tag filter, twin of models list -tag
			keep := list[:0]
			for _, s := range list {
				for _, t := range s.Manifest.Tags {
					if t == *tagF {
						keep = append(keep, s)
						break
					}
				}
			}
			list = keep
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
		if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F489)
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
		sfs := flag.NewFlagSet("skills show", flag.ExitOnError)
		sout := sfs.String("out", "", "write the skill JSON to this file instead of stdout (F619)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl skills show <id> [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/skills/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("skill %s: %s", sfs.Arg(0), out)
		}
		if *sout != "" { // F619: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*sout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *sout, len(out))
			return nil
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
		seds := flag.NewFlagSet("skills "+sub, flag.ExitOnError)
		sejson := seds.Bool("json", false, "print the raw response JSON (F537)")
		seds.Parse(reorderFlags(rest))
		if seds.NArg() == 0 {
			return fmt.Errorf("usage: llmctl skills %s <id> [-json]", sub)
		}
		body := map[string]any{"enabled": sub == "enable"}
		code, out, err := cc.Post("/api/v1/skills/"+url.PathEscape(seds.Arg(0))+"/enabled", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s returned %d: %s", sub, code, out)
		}
		if *sejson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("%sd skill %s\n", sub, seds.Arg(0))
		return nil
	case "delete":
		sdfs := flag.NewFlagSet("skills delete", flag.ExitOnError)
		sdjson := sdfs.Bool("json", false, "print the raw response JSON (F536)")
		sdfs.Parse(reorderFlags(rest))
		if sdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl skills delete <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/skills/" + url.PathEscape(sdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		if *sdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted skill %s\n", sdfs.Arg(0))
		return nil
	default:
		return fmt.Errorf("unknown skills subcommand %q (list|show|add|export|enable|disable|delete)", sub)
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
		case "list", "show", "add", "update", "delete", "search", "export", "import", "retag":
			sub, rest = args[0], args[1:]
		}
	}
	switch sub {
	case "retag":
		// Bulk-rename or drop a tag across all entries (F640).
		rfs := flag.NewFlagSet("memory retag", flag.ExitOnError)
		rm := rfs.Bool("rm", false, "drop the tag instead of renaming it")
		rjson := rfs.Bool("json", false, "print the count as JSON")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() < 1 || (!*rm && rfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl memory retag <old-tag> <new-tag> | memory retag -rm <tag> [-json]")
		}
		oldTag := rfs.Arg(0)
		newTag := rfs.Arg(1)
		code, out, err := cc.Get("/api/v1/memory")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory returned %d: %s", code, out)
		}
		var mems []struct {
			ID    string `json:"id"`
			Entry struct {
				Content string   `json:"content"`
				Tags    []string `json:"tags"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(out, &mems); err != nil {
			return err
		}
		changed := 0
		for _, m := range mems {
			found := false
			var tags []string
			for _, t := range m.Entry.Tags {
				if t != oldTag {
					tags = append(tags, t)
					continue
				}
				found = true
				if !*rm {
					tags = append(tags, newTag)
				}
			}
			if !found {
				continue
			}
			body := map[string]any{"tags": tags, "content": m.Entry.Content}
			code, out, err := cc.Do("PUT", "/api/v1/memory/"+url.PathEscape(m.ID), body)
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("memory retag on %s: %s", m.ID, out)
			}
			changed++
		}
		if *rjson {
			fmt.Printf("{\"changed\":%d}\n", changed)
			return nil
		}
		word := "ies"
		if changed == 1 {
			word = "y"
		}
		if *rm {
			fmt.Printf("removed tag %q from %d entr%s\n", oldTag, changed, word)
			return nil
		}
		fmt.Printf("renamed tag %q -> %q on %d entr%s\n", oldTag, newTag, changed, word)
		return nil
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
		mijson := ifs.Bool("json", false, "print {\"created\",\"skipped\"} as JSON (F542)")
		ifs.Parse(reorderFlags(rest))
		if ifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory import <file.json> [-json]")
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
				if *mijson {
					fmt.Fprintf(os.Stderr, "  skip: %s\n", out)
				} else {
					fmt.Printf("  skip: %s\n", out)
				}
				skipped++
				continue
			}
			created++
		}
		if *mijson {
			b, _ := json.Marshal(map[string]int{"created": created, "skipped": skipped})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("imported %d entries (%d skipped)\n", created, skipped)
		return nil
	case "update":
		ufs := flag.NewFlagSet("memory update", flag.ExitOnError)
		content := ufs.String("content", "", "new content")
		tags := ufs.String("tags", "", "comma-separated tags (replaces)")
		mujson := ufs.Bool("json", false, "print the raw response JSON (F539)")
		ufs.Parse(reorderFlags(rest))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory update <id> [-content C] [-tags a,b] [-json]")
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
		if *mujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("memory entry %s updated\n", ufs.Arg(0))
		return nil
	case "search":
		sfs := flag.NewFlagSet("memory search", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw search JSON (for scripts)")
		scsv := sfs.Bool("csv", false, "output the matches as CSV (F617)")
		sout := sfs.String("out", "", "write the raw search JSON to this file instead of stdout (F484)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory search <query> [-json] [-csv] [-out FILE]")
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
		if *scsv { // F617: tags ;-joined like `memory list --csv` (F401)
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "tags", "content"})
			for _, r := range res {
				_ = w.Write([]string{r.Entry.ID, strings.Join(r.Entry.Tags, ";"), r.Entry.Content})
			}
			w.Flush()
			return w.Error()
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
		outFile := fs.String("out", "", "write the memory entries JSON to this file instead of stdout (F490)")
		stats := fs.Bool("stats", false, "aggregate the entries per tag instead of listing them (F626)")
		tagF := fs.String("tag", "", "only entries carrying this tag (F674)")
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
		if *tagF != "" { // F674: exact tag filter, twin of models list -tag
			keep := list[:0]
			for _, m := range list {
				for _, t := range m.Entry.Tags {
					if t == *tagF {
						keep = append(keep, m)
						break
					}
				}
			}
			list = keep
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
		if *stats { // F626: one row per tag (entries, last created); "(none)" groups untagged
			type mst struct {
				Tag     string `json:"tag"`
				Entries int    `json:"entries"`
				Last    string `json:"last"`
			}
			byTag := map[string]*mst{}
			lastT := map[string]time.Time{}
			for _, m := range list {
				keys := m.Entry.Tags
				if len(keys) == 0 {
					keys = []string{"(none)"}
				}
				t, _ := time.Parse(time.RFC3339, m.Entry.CreatedAt)
				for _, tag := range keys {
					g := byTag[tag]
					if g == nil {
						g = &mst{Tag: tag}
						byTag[tag] = g
					}
					g.Entries++
					if t.After(lastT[tag]) {
						lastT[tag] = t
					}
				}
			}
			names := make([]string, 0, len(byTag))
			for k := range byTag {
				names = append(names, k)
			}
			sort.Slice(names, func(i, j int) bool { return byTag[names[i]].Entries > byTag[names[j]].Entries })
			rows := make([]mst, 0, len(names))
			for _, k := range names {
				g := byTag[k]
				if !lastT[k].IsZero() {
					g.Last = lastT[k].UTC().Format(time.RFC3339)
				}
				rows = append(rows, *g)
			}
			if *jsonOut || *outFile != "" {
				b, _ := json.Marshal(rows)
				if *outFile != "" {
					if err := os.WriteFile(*outFile, b, 0o644); err != nil {
						return err
					}
					fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
					return nil
				}
				fmt.Println(string(b))
				return nil
			}
			if *csvOut {
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"tag", "entries", "last"})
				for _, r := range rows {
					_ = w.Write([]string{r.Tag, strconv.Itoa(r.Entries), r.Last})
				}
				w.Flush()
				return w.Error()
			}
			if len(rows) == 0 {
				fmt.Println("no memory entries")
				return nil
			}
			fmt.Printf("%-20s %8s %-20s\n", "TAG", "ENTRIES", "LAST (UTC)")
			for _, r := range rows {
				last := r.Last
				if t, err := time.Parse(time.RFC3339, r.Last); err == nil {
					last = t.Format("2006-01-02 15:04:05")
				}
				fmt.Printf("%-20s %8d %-20s\n", r.Tag, r.Entries, last)
			}
			return nil
		}
		if len(list) > *n {
			list = list[:*n]
		}
		if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F490)
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
		mfs := flag.NewFlagSet("memory show", flag.ExitOnError)
		mout := mfs.String("out", "", "write the entry JSON to this file instead of stdout (F619)")
		mfs.Parse(reorderFlags(rest))
		if mfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory show <id> [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/memory/" + url.PathEscape(mfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("memory %s: %s", mfs.Arg(0), out)
		}
		if *mout != "" { // F619: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*mout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *mout, len(out))
			return nil
		}
		fmt.Print(string(out))
		return nil
	case "add":
		afs := flag.NewFlagSet("memory add", flag.ExitOnError)
		tags := afs.String("tags", "", "comma-separated tags")
		mjson := afs.Bool("json", false, "print the raw created-entry JSON (F538)")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory add \"<text>\" [-tags a,b] [-json]")
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
		if *mjson {
			fmt.Print(string(out))
			return nil
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
		mdfs := flag.NewFlagSet("memory delete", flag.ExitOnError)
		mdjson := mdfs.Bool("json", false, "print the raw response JSON (F536)")
		mdfs.Parse(reorderFlags(rest))
		if mdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl memory delete <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/memory/" + url.PathEscape(mdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		if *mdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted memory %s\n", mdfs.Arg(0))
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
		case "list", "show", "create", "update", "tag", "retag", "docs", "doc", "add-doc", "export", "search", "search-all", "embed", "delete":
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
		ejson := efs.Bool("json", false, "print the raw response JSON (F541)")
		efs.Parse(reorderFlags(rest))
		if efs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb embed <kb-id> [-url U -model M [-key K]] | -clear [-json]")
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
		if *ejson {
			fmt.Print(string(out))
			return nil
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
		scsv := sfs.Bool("csv", false, "output the matches as CSV (F617)")
		limit := sfs.Int("limit", 10, "max results")
		sout := sfs.String("out", "", "write the raw search JSON to this file instead of stdout (F483)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl kb search <kb-id> <query> [-limit N] [-json] [-csv] [-out FILE]")
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
		if *scsv { // F617: full chunk text, score to 3 decimals like the table
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"doc_id", "filename", "chunk_index", "score", "chunk_text"})
			for _, r := range res {
				_ = w.Write([]string{r.DocID, r.Filename, strconv.Itoa(r.ChunkIndex),
					strconv.FormatFloat(r.Score, 'f', 3, 64), r.ChunkText})
			}
			w.Flush()
			return w.Error()
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
	case "search-all":
		// One query across every KB, merged by score (F665).
		sfs := flag.NewFlagSet("kb search-all", flag.ExitOnError)
		sjson := sfs.Bool("json", false, "print the raw search JSON (for scripts)")
		scsv := sfs.Bool("csv", false, "output the matches as CSV")
		limit := sfs.Int("limit", 10, "max results")
		sout := sfs.String("out", "", "write the raw search JSON to this file instead of stdout")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl kb search-all <query> [-limit N] [-json] [-csv] [-out FILE]")
		}
		query := strings.Join(sfs.Args(), " ")
		code, out, err := cc.Post("/api/v1/knowledge-bases/search",
			map[string]any{"q": query, "limit": *limit})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb search-all: %s", out)
		}
		if *sout != "" {
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
		var res struct {
			Results []struct {
				KBID       string  `json:"kb_id"`
				KBName     string  `json:"kb_name"`
				DocID      string  `json:"doc_id"`
				Filename   string  `json:"filename"`
				ChunkText  string  `json:"chunk_text"`
				Score      float64 `json:"score"`
				ChunkIndex int     `json:"chunk_index"`
			} `json:"results"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return err
		}
		if *scsv {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"kb_id", "kb_name", "doc_id", "filename", "chunk_index", "score", "chunk_text"})
			for _, r := range res.Results {
				_ = w.Write([]string{r.KBID, r.KBName, r.DocID, r.Filename, strconv.Itoa(r.ChunkIndex),
					strconv.FormatFloat(r.Score, 'f', 3, 64), r.ChunkText})
			}
			w.Flush()
			return w.Error()
		}
		if len(res.Results) == 0 {
			fmt.Printf("no chunks match %q in any knowledge base\n", query)
			return nil
		}
		for _, r := range res.Results {
			sn := strings.TrimSpace(r.ChunkText)
			sn = strings.ReplaceAll(sn, "\n", " ")
			if len(sn) > 80 {
				sn = sn[:80] + "…"
			}
			fmt.Printf("%-16s %-24s chunk %-4d %6.3f  %s\n", r.KBName, r.Filename, r.ChunkIndex, r.Score, sn)
		}
		return nil
	case "list":
		fs := flag.NewFlagSet("kb", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the knowledge bases as JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the knowledge-base list as CSV (F379)")
		outFile := fs.String("out", "", "write the knowledge-base JSON to this file instead of stdout (F491)")
		tagF := fs.String("tag", "", "only knowledge bases carrying this tag (F674)")
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
				ID          string   `json:"id"`
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Tags        []string `json:"tags"`
				DocCount    int      `json:"doc_count"`
			} `json:"kb"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if *tagF != "" { // F674: exact tag filter, twin of models list -tag
			keep := list[:0]
			for _, kb := range list {
				for _, t := range kb.KB.Tags {
					if t == *tagF {
						keep = append(keep, kb)
						break
					}
				}
			}
			list = keep
		}
		if filter != "" {
			var keep []struct {
				KB struct {
					ID          string   `json:"id"`
					Name        string   `json:"name"`
					Description string   `json:"description"`
					Tags        []string `json:"tags"`
					DocCount    int      `json:"doc_count"`
				} `json:"kb"`
			}
			for _, kb := range list {
				hay := kb.KB.Name + " " + kb.KB.Description + " " + strings.Join(kb.KB.Tags, " ")
				if strings.Contains(strings.ToLower(hay), filter) {
					keep = append(keep, kb)
				}
			}
			list = keep
		}
		if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F491)
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
			if *csvOut { // keep the stream parseable (F379)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "name", "docs", "description", "tags"})
				w.Flush()
				return w.Error()
			}
			fmt.Println("no knowledge bases")
			return nil
		}
		if *csvOut { // F379
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "name", "docs", "description", "tags"})
			for _, kb := range list {
				_ = w.Write([]string{kb.KB.ID, kb.KB.Name, strconv.Itoa(kb.KB.DocCount), kb.KB.Description, strings.Join(kb.KB.Tags, ";")})
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
		sout := sfs.String("out", "", "write the raw KB JSON to this file instead of stdout (F505)")
		sfs.Parse(reorderFlags(rest))
		if sfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb show <id> [-json] [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(sfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb %s: %s", sfs.Arg(0), out)
		}
		if *sout != "" { // F505: raw JSON to a file, stdout stays clean
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
		// Human view (F366): header, search mode, then the document list.
		var rec struct {
			ID  string `json:"id"`
			Dir string `json:"dir"`
			KB  struct {
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Tags        []string `json:"tags"`
				DocCount    int      `json:"doc_count"`
				CreatedAt   string   `json:"created_at"`
				UpdatedAt   string   `json:"updated_at"`
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
		if len(rec.KB.Tags) > 0 {
			fmt.Printf("  tags:        %s\n", strings.Join(rec.KB.Tags, ", "))
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
		dfs := flag.NewFlagSet("kb docs", flag.ExitOnError)
		djson := dfs.Bool("json", false, "print the raw documents JSON (for scripts) (F618)")
		dcsv := dfs.Bool("csv", false, "output the document list as CSV (F618)")
		dout := dfs.String("out", "", "write the raw documents JSON to this file instead of stdout (F618)")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb docs <id> [-json] [-csv] [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/knowledge-bases/" + url.PathEscape(dfs.Arg(0)) + "/documents")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb docs returned %d: %s", code, out)
		}
		if *dout != "" { // F618: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*dout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *dout, len(out))
			return nil
		}
		if *djson {
			fmt.Print(string(out))
			return nil
		}
		var docs []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}
		if err := json.Unmarshal(out, &docs); err != nil {
			return err
		}
		if *dcsv { // F618: header-only when empty keeps the stream parseable
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"id", "filename", "size_bytes"})
			for _, d := range docs {
				_ = w.Write([]string{d.ID, d.Filename, strconv.FormatInt(d.Size, 10)})
			}
			w.Flush()
			return w.Error()
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
		adjson := dfs.Bool("json", false, "print the raw created-doc JSON (F539)")
		dfs.Parse(reorderFlags(rest))
		if dfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl kb add-doc <kb-id> <path> [-name X] [-json]")
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
		if *adjson {
			fmt.Print(string(out))
			return nil
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
		kujson := ufs.Bool("json", false, "print the raw response JSON (F539)")
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
		if *kujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("knowledge base %s updated\n", rest[0])
		return nil
	case "tag":
		// Replace a KB's tags (comma-separated); empty clears (F664).
		tfs := flag.NewFlagSet("kb tag", flag.ExitOnError)
		tj := tfs.Bool("json", false, "print the raw response JSON")
		tfs.Parse(reorderFlags(rest))
		if tfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl kb tag <id> [a,b,c] [-json]  (empty clears)")
		}
		var tags []string
		if tfs.NArg() > 1 {
			for _, t := range strings.Split(tfs.Arg(1), ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
		code, out, err := cc.Do("PATCH", "/api/v1/knowledge-bases/"+url.PathEscape(tfs.Arg(0))+"/tags",
			map[string]any{"tags": tags})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb tag: %s", out)
		}
		if *tj {
			fmt.Print(string(out))
			return nil
		}
		if len(tags) == 0 {
			fmt.Printf("knowledge base %s tags cleared\n", tfs.Arg(0))
		} else {
			fmt.Printf("knowledge base %s tags: %s\n", tfs.Arg(0), strings.Join(tags, ", "))
		}
		return nil
	case "retag":
		// Bulk-rename or drop a tag across all KBs (F664), twin of
		// `memory retag`; the tag rules live in the tags endpoint.
		rfs := flag.NewFlagSet("kb retag", flag.ExitOnError)
		rm := rfs.Bool("rm", false, "drop the tag instead of renaming it")
		rjson := rfs.Bool("json", false, "print the count as JSON")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() < 1 || (!*rm && rfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl kb retag <old-tag> <new-tag> | kb retag -rm <tag> [-json]")
		}
		oldTag, newTag := rfs.Arg(0), rfs.Arg(1)
		code, out, err := cc.Get("/api/v1/knowledge-bases")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("kb list returned %d: %s", code, out)
		}
		var kbs []struct {
			ID string `json:"id"`
			KB struct {
				Tags []string `json:"tags"`
			} `json:"kb"`
		}
		if err := json.Unmarshal(out, &kbs); err != nil {
			return err
		}
		changed := 0
		for _, k := range kbs {
			found := false
			var tags []string
			for _, t := range k.KB.Tags {
				if t != oldTag {
					tags = append(tags, t)
					continue
				}
				found = true
				if !*rm {
					tags = append(tags, newTag)
				}
			}
			if !found {
				continue
			}
			code, out, err := cc.Do("PATCH", "/api/v1/knowledge-bases/"+url.PathEscape(k.ID)+"/tags",
				map[string]any{"tags": tags})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("kb retag %s: %s", k.ID, out)
			}
			changed++
		}
		if *rjson {
			fmt.Printf("{\"changed\":%d}\n", changed)
			return nil
		}
		if *rm {
			fmt.Printf("dropped tag %q from %d knowledge base(s)\n", oldTag, changed)
		} else {
			fmt.Printf("renamed tag %q to %q on %d knowledge base(s)\n", oldTag, newTag, changed)
		}
		return nil
	case "create":
		cfs := flag.NewFlagSet("kb create", flag.ExitOnError)
		desc := cfs.String("desc", "", "description")
		kcjson := cfs.Bool("json", false, "print the raw created-kb JSON (F538)")
		cfs.Parse(reorderFlags(rest))
		if cfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb create <name> [-desc X] [-json]")
		}
		code, out, err := cc.Post("/api/v1/knowledge-bases", map[string]any{"name": cfs.Arg(0), "description": *desc})
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("kb create returned %d: %s", code, out)
		}
		if *kcjson {
			fmt.Print(string(out))
			return nil
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
		kdfs := flag.NewFlagSet("kb delete", flag.ExitOnError)
		kdjson := kdfs.Bool("json", false, "print the raw response JSON (F536)")
		kdfs.Parse(reorderFlags(rest))
		if kdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl kb delete <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/knowledge-bases/" + url.PathEscape(kdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		if *kdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted knowledge base %s\n", kdfs.Arg(0))
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
		case "list", "show", "install", "enable", "disable", "sandbox", "run", "delete":
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
		tifs := flag.NewFlagSet("tools install", flag.ExitOnError)
		tijson := tifs.Bool("json", false, "print the raw installed-tool JSON (F540)")
		tiid := tifs.String("id", "", "install under this id instead of the derived one (F657)")
		tifs.Parse(reorderFlags(rest))
		if tifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl tools install <dir|https://…/tool.zip> [-id ID] [-json]")
		}
		body := map[string]any{}
		if arg := tifs.Arg(0); strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			body["url"] = arg // F657: install a packaged .zip over HTTP, like the Tools page
		} else {
			abs, err := filepath.Abs(arg)
			if err != nil {
				return err
			}
			body["dir"] = abs
		}
		if *tiid != "" {
			body["id"] = *tiid
		}
		code, out, err := cc.Post("/api/v1/tools/install", body)
		if err != nil {
			return err
		}
		if code != 201 && code != 200 {
			return fmt.Errorf("install returned %d: %s", code, out)
		}
		if *tijson {
			fmt.Print(string(out))
			return nil
		}
		var rec struct {
			ID string `json:"id"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("installed tool %s\n", rec.ID)
		return nil
	case "show":
		tfs := flag.NewFlagSet("tools show", flag.ExitOnError)
		tout := tfs.String("out", "", "write the tool JSON to this file instead of stdout (F619)")
		tfs.Parse(reorderFlags(rest))
		if tfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl tools show <id> [-out FILE]")
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
			if t["id"] == tfs.Arg(0) {
				b, _ := json.MarshalIndent(t, "", "  ")
				if *tout != "" { // F619: JSON to a file, stdout stays clean
					if err := os.WriteFile(*tout, append(b, '\n'), 0o644); err != nil {
						return err
					}
					fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *tout, len(b)+1)
					return nil
				}
				fmt.Println(string(b))
				return nil
			}
		}
		return fmt.Errorf("no such tool: %s", tfs.Arg(0))
	case "enable", "disable":
		teds := flag.NewFlagSet("tools "+sub, flag.ExitOnError)
		tejson := teds.Bool("json", false, "print the raw response JSON (F537)")
		teds.Parse(reorderFlags(rest))
		if teds.NArg() == 0 {
			return fmt.Errorf("usage: llmctl tools %s <id> [-json]", sub)
		}
		body := map[string]any{"enabled": sub == "enable"}
		code, out, err := cc.Post("/api/v1/tools/"+url.PathEscape(teds.Arg(0))+"/enabled", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s returned %d: %s", sub, code, out)
		}
		if *tejson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("%sd tool %s\n", sub, teds.Arg(0))
		return nil
	case "sandbox":
		// Toggle sandboxed execution for a python tool (POST sandbox, F653),
		// the CLI twin of the Tools page sandbox toggle.
		tsfs := flag.NewFlagSet("tools sandbox", flag.ExitOnError)
		tsjson := tsfs.Bool("json", false, "print the raw response JSON")
		tsfs.Parse(reorderFlags(rest))
		if tsfs.NArg() != 2 {
			return fmt.Errorf("usage: llmctl tools sandbox <id> <on|off> [-json]")
		}
		s := strings.ToLower(tsfs.Arg(1))
		if s != "on" && s != "off" {
			return fmt.Errorf("expected on or off, got %q", tsfs.Arg(1))
		}
		code, out, err := cc.Post("/api/v1/tools/"+url.PathEscape(tsfs.Arg(0))+"/sandbox",
			map[string]any{"sandbox": s == "on"})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("tools sandbox returned %d: %s", code, out)
		}
		if *tsjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("tool %s sandbox %s\n", tsfs.Arg(0), s)
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
		tdfs := flag.NewFlagSet("tools delete", flag.ExitOnError)
		tdjson := tdfs.Bool("json", false, "print the raw response JSON (F536)")
		tdfs.Parse(reorderFlags(rest))
		if tdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl tools delete <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/tools/" + url.PathEscape(tdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		if *tdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted tool %s\n", tdfs.Arg(0))
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
		mifs := flag.NewFlagSet("mcp import", flag.ExitOnError)
		mijson2 := mifs.Bool("json", false, "print {\"created\",\"skipped\"} as JSON (F542)")
		mifs.Parse(reorderFlags(rest))
		if mifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl mcp import <file.json> [-json]")
		}
		data, err := os.ReadFile(mifs.Arg(0))
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
				if *mijson2 {
					fmt.Fprintf(os.Stderr, "  skip %v: %s\n", s["name"], out)
				} else {
					fmt.Printf("  skip %v: %s\n", s["name"], out)
				}
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
		if *mijson2 {
			b, _ := json.Marshal(map[string]int{"created": created, "skipped": skipped})
			fmt.Println(string(b))
			return nil
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
		aout := afs.String("out", "", "write the raw created-server JSON to this file instead of stdout (F511)")
		ajson := afs.Bool("json", false, "print the raw created-server JSON (F655)")
		var aenv stringList
		afs.Var(&aenv, "env", "server environment variable k=v (repeatable, F655)")
		afs.Parse(reorderFlags(rest))
		if afs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl mcp add <name> <command> [-args 'a b'] [-env k=v ...] [-json] [-out FILE]")
		}
		body := map[string]any{"name": afs.Arg(0), "command": afs.Arg(1)}
		if *sargs != "" {
			var list []string
			for _, a := range strings.Fields(*sargs) {
				list = append(list, a)
			}
			body["args"] = list
		}
		if m := aenv.Map(); len(m) > 0 {
			body["env"] = m
		}
		code, out, err := cc.Post("/api/v1/mcp", body)
		if err != nil {
			return err
		}
		if code != 201 && code != 200 {
			return fmt.Errorf("mcp add returned %d: %s", code, out)
		}
		if *aout != "" { // F511: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*aout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *aout, len(out))
			return nil
		}
		if *ajson {
			fmt.Print(string(out))
			return nil
		}
		var rec struct {
			ID string `json:"id"`
		}
		json.Unmarshal(out, &rec)
		fmt.Printf("added MCP server %s\n", rec.ID)
		return nil
	case "remove":
		mrfs := flag.NewFlagSet("mcp remove", flag.ExitOnError)
		mrjson := mrfs.Bool("json", false, "print the raw response JSON (F536)")
		mrfs.Parse(reorderFlags(rest))
		if mrfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl mcp remove <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/mcp/" + url.PathEscape(mrfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("remove returned %d: %s", code, out)
		}
		if *mrjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("removed MCP server %s\n", mrfs.Arg(0))
		return nil
	case "enable", "disable":
		meds := flag.NewFlagSet("mcp "+sub, flag.ExitOnError)
		mejson := meds.Bool("json", false, "print the raw response JSON (F537)")
		meds.Parse(reorderFlags(rest))
		if meds.NArg() == 0 {
			return fmt.Errorf("usage: llmctl mcp %s <id> [-json]", sub)
		}
		body := map[string]any{"enabled": sub == "enable"}
		code, out, err := cc.Post("/api/v1/mcp/"+url.PathEscape(meds.Arg(0))+"/enabled", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s returned %d: %s", sub, code, out)
		}
		if *mejson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("%sd MCP server %s\n", sub, meds.Arg(0))
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
// cmdCost prints estimated costs; -watch N reprints the view every N seconds
// until interrupted (F588), like usage -watch.
func cmdCost(args []string) error {
	watch := 0
	for i, a := range args {
		if a == "-watch" && i+1 < len(args) {
			watch, _ = strconv.Atoi(args[i+1])
		} else if strings.HasPrefix(a, "-watch=") {
			watch, _ = strconv.Atoi(strings.TrimPrefix(a, "-watch="))
		}
	}
	if watch <= 0 {
		return costOnce(args)
	}
	for {
		fmt.Print("\x1b[2J\x1b[H") // clear screen
		fmt.Printf("live cost — refreshing every %ds (Ctrl-C to stop)\n\n", watch)
		if err := costOnce(args); err != nil {
			return err
		}
		time.Sleep(time.Duration(watch) * time.Second)
	}
}

func costOnce(args []string) error {
	fs := flag.NewFlagSet("cost", flag.ExitOnError)
	byKey := fs.Bool("keys", false, "show per-key costs instead of per-model")
	modelF := fs.String("model", "", "show only this model id (F349)")
	keyF := fs.String("key", "", "with -keys: show only this key id (F349)")
	csvOut := fs.Bool("csv", false, "output the cost table as CSV (F370)")
	jsonOut := fs.Bool("json", false, "print the raw cost JSON (for scripts)")
	outFile := fs.String("out", "", "write the cost JSON to this file instead of stdout (F470)")
	fs.Int("watch", 0, "refresh every N seconds until Ctrl-C (0 = print once) (F588)")
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
	if len(args) > 0 && args[0] == "duplicate" {
		// Copy an existing template (built-in or custom) to a new custom id (F638).
		dfs := flag.NewFlagSet("prompts duplicate", flag.ExitOnError)
		name := dfs.String("name", "", "new display name (default: original + \" copy\")")
		category := dfs.String("category", "", "new category (default: original)")
		djson := dfs.Bool("json", false, "print the raw created-prompt JSON")
		dfs.Parse(reorderFlags(args[1:]))
		if dfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl prompts duplicate <id> <new-id> [-name N] [-category C] [-json]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Get("/api/v1/prompts/" + url.PathEscape(dfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompt %s not found (%d)", dfs.Arg(0), code)
		}
		var src struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Content     string `json:"content"`
		}
		if err := json.Unmarshal(out, &src); err != nil {
			return err
		}
		newName := *name
		if newName == "" {
			newName = src.Name + " copy"
		}
		newCat := *category
		if newCat == "" {
			newCat = src.Category
		}
		code, out, err = cc.Post("/api/v1/prompts", map[string]any{
			"id": dfs.Arg(1), "name": newName, "category": newCat,
			"description": src.Description, "content": src.Content,
		})
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("prompts duplicate: %s", out)
		}
		if *djson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("prompt duplicated: %s -> %s (%s)\n", dfs.Arg(0), dfs.Arg(1), newName)
		return nil
	}
	if len(args) > 0 && args[0] == "retag" {
		// Bulk-rename a category across all custom templates (F641).
		rfs := flag.NewFlagSet("prompts retag", flag.ExitOnError)
		rjson := rfs.Bool("json", false, "print the count as JSON")
		rfs.Parse(reorderFlags(args[1:]))
		if rfs.NArg() < 2 {
			return fmt.Errorf("usage: llmctl prompts retag <old-category> <new-category> [-json]")
		}
		oldCat := rfs.Arg(0)
		newCat := rfs.Arg(1)
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
		var list []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Content     string `json:"content"`
			Custom      bool   `json:"custom"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		changed := 0
		for _, p := range list {
			if !p.Custom || !strings.EqualFold(p.Category, oldCat) {
				continue // built-ins are read-only
			}
			code, out, err := cc.Do("PUT", "/api/v1/prompts/"+url.PathEscape(p.ID), map[string]any{
				"name": p.Name, "category": newCat, "description": p.Description, "content": p.Content,
			})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("prompts retag on %s: %s", p.ID, out)
			}
			changed++
		}
		if *rjson {
			fmt.Printf("{\"changed\":%d}\n", changed)
			return nil
		}
		fmt.Printf("renamed category %q -> %q on %d custom template(s)\n", oldCat, newCat, changed)
		return nil
	}
	if len(args) > 0 && args[0] == "add" {
		// Create a custom prompt template (F306).
		afs := flag.NewFlagSet("prompts add", flag.ExitOnError)
		name := afs.String("name", "", "display name (required)")
		category := afs.String("category", "", "category (optional)")
		desc := afs.String("desc", "", "description (optional)")
		content := afs.String("content", "", "template text (or use -file / piped stdin)")
		file := afs.String("file", "", "read the template text from this file")
		pajson := afs.Bool("json", false, "print the raw created-prompt JSON (F544)")
		afs.Parse(reorderFlags(args[1:]))
		if afs.NArg() == 0 || *name == "" {
			return fmt.Errorf("usage: llmctl prompts add <id> -name N [-category C] [-desc D] (-content TEXT | -file F | piped stdin) [-json]")
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
		if *pajson {
			fmt.Print(string(out))
			return nil
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
		pujson := ufs.Bool("json", false, "print the raw response JSON (F539)")
		ufs.Parse(reorderFlags(args[1:]))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl prompts update <id> [-name N] [-category C] [-desc D] [-content T | -file F | piped stdin] [-json]")
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
		if *pujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("updated prompt %s\n", ufs.Arg(0))
		return nil
	}
	if len(args) > 0 && args[0] == "delete" {
		prdfs := flag.NewFlagSet("prompts delete", flag.ExitOnError)
		prdjson := prdfs.Bool("json", false, "print the raw response JSON (F538)")
		prdfs.Parse(reorderFlags(args[1:]))
		if prdfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl prompts delete <id> [-json]")
		}
		cc, err := daemon.NewControlClient()
		if err != nil {
			return err
		}
		code, out, err := cc.Delete("/api/v1/prompts/" + url.PathEscape(prdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("prompts delete: %s", out)
		}
		if *prdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted prompt %s\n", prdfs.Arg(0))
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
		prifs := flag.NewFlagSet("prompts import", flag.ExitOnError)
		prjson := prifs.Bool("json", false, "print {\"created\",\"skipped\"} as JSON (F542)")
		prifs.Parse(reorderFlags(args[1:]))
		if prifs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl prompts import <file.json> [-json]")
		}
		data, err := os.ReadFile(prifs.Arg(0))
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
				if *prjson {
					fmt.Fprintf(os.Stderr, "  skip %s: %s\n", e.ID, out)
				} else {
					fmt.Printf("  skip %s: %s\n", e.ID, out)
				}
				skipped++
				continue
			}
			created++
		}
		if *prjson {
			b, _ := json.Marshal(map[string]int{"created": created, "skipped": skipped})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("imported %d prompts (%d skipped)\n", created, skipped)
		return nil
	}
	if len(args) > 0 && args[0] == "categories" {
		cfs := flag.NewFlagSet("prompts categories", flag.ExitOnError)
		cjson := cfs.Bool("json", false, "print the raw categories JSON (F595)")
		ccsv := cfs.Bool("csv", false, "output the categories as CSV (F616)")
		cout := cfs.String("out", "", "write the categories JSON to this file instead of stdout (F595)")
		cfs.Parse(reorderFlags(args[1:]))
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
		if *cout != "" { // F595: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*cout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *cout, len(out))
			return nil
		}
		if *cjson {
			fmt.Print(string(out))
			return nil
		}
		var cats []string
		if err := json.Unmarshal(out, &cats); err != nil {
			fmt.Print(string(out))
			return nil
		}
		if *ccsv { // F616: one column, header-only when empty
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"category"})
			for _, c := range cats {
				_ = w.Write([]string{c})
			}
			w.Flush()
			return w.Error()
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
	if len(args) > 0 && args[0] == "run" { // F579: render the template and send it to a model
		rfs := flag.NewFlagSet("prompts run", flag.ExitOnError)
		input := rfs.String("input", "", "text to substitute for {{input}} (default: piped stdin)")
		var vlist stringList
		rfs.Var(&vlist, "var", "substitute {{k}} for v (repeatable)")
		modelF := rfs.String("model", "", "model id to send to (required)")
		keyF := rfs.String("key", os.Getenv("LLMCTL_API_KEY"), "gateway API key (if auth is enabled)")
		system := rfs.String("system", "", "system message prepended to the request")
		temp := rfs.Float64("temperature", -1, "sampling temperature (-1 = model default)")
		maxTok := rfs.Int("max-tokens", 0, "max tokens to generate (0 = model default)")
		saveChat := rfs.String("save", "", "save the exchange as a saved chat under this title (F583)")
		contChat := rfs.String("chat", "", "continue this saved chat — its history is sent and the reply is appended (F584)")
		rjson := rfs.Bool("json", false, "print the raw gateway JSON instead of the reply text")
		rout := rfs.String("out", "", "write the reply text to this file instead of stdout (F614)")
		rfs.Parse(reorderFlags(args[1:]))
		if rfs.NArg() == 0 || *modelF == "" {
			return fmt.Errorf("usage: llmctl prompts run <id> -model M [-input TEXT] [-var k=v ...] [-system S] [-save TITLE | -chat ID] [-key K] [-json] [-out FILE]")
		}
		if *saveChat != "" && *contChat != "" {
			return fmt.Errorf("-save and -chat are mutually exclusive")
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
		rendered := strings.ReplaceAll(p.Content, "{{input}}", text)
		for _, kv := range vlist {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return fmt.Errorf("-var expects k=v, got %q", kv)
			}
			rendered = strings.ReplaceAll(rendered, "{{"+strings.TrimSpace(k)+"}}", v)
		}
		var hist []map[string]string
		contTitle := ""
		if *contChat != "" { // F584: continue a saved chat
			ccode, cout, err := cc.Get("/api/v1/chats/" + url.PathEscape(*contChat))
			if err != nil {
				return err
			}
			if ccode != 200 {
				return fmt.Errorf("chat %s: %s", *contChat, cout)
			}
			var ch struct {
				Title    string `json:"title"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(cout, &ch); err != nil {
				return err
			}
			contTitle = ch.Title
			for _, m := range ch.Messages {
				hist = append(hist, map[string]string{"role": m.Role, "content": m.Content})
			}
		}
		msgs := []map[string]any{}
		if strings.TrimSpace(*system) != "" && len(hist) == 0 {
			msgs = append(msgs, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
		}
		for _, m := range hist {
			msgs = append(msgs, map[string]any{"role": m["role"], "content": m["content"]})
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": rendered})
		reqBody := map[string]any{"model": *modelF, "messages": msgs}
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
		if *keyF != "" {
			req.Header.Set("Authorization", "Bearer "+*keyF)
		}
		resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return fmt.Errorf("chat returned %d: %s", resp.StatusCode, string(body))
		}
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Choices) == 0 {
			return fmt.Errorf("unexpected gateway response: %s", body)
		}
		if *rout != "" { // F614: reply text to a file, stdout stays clean
			content := parsed.Choices[0].Message.Content
			if *rjson {
				content = string(body)
			}
			if err := os.WriteFile(*rout, []byte(content+"\n"), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *rout, len(content)+1)
		} else if *rjson {
			fmt.Print(string(body))
		} else {
			fmt.Println(parsed.Choices[0].Message.Content)
		}
		if *contChat != "" { // F584: append the exchange to the continued chat
			smsgs := append(hist,
				map[string]string{"role": "user", "content": rendered},
				map[string]string{"role": "assistant", "content": parsed.Choices[0].Message.Content})
			scode, sout, err := cc.Put("/api/v1/chats/"+url.PathEscape(*contChat), map[string]any{
				"title": contTitle, "model": *modelF, "messages": smsgs,
			})
			if err != nil {
				return err
			}
			if scode != 200 {
				return fmt.Errorf("chat update: %s", sout)
			}
			fmt.Fprintf(os.Stderr, "appended to chat %s\n", *contChat)
		} else if *saveChat != "" { // F583: keep the exchange like chat -save does
			smsgs := []map[string]string{}
			if strings.TrimSpace(*system) != "" {
				smsgs = append(smsgs, map[string]string{"role": "system", "content": strings.TrimSpace(*system)})
			}
			smsgs = append(smsgs,
				map[string]string{"role": "user", "content": rendered},
				map[string]string{"role": "assistant", "content": parsed.Choices[0].Message.Content})
			scode, sout, err := cc.Post("/api/v1/chats", map[string]any{
				"title": *saveChat, "model": *modelF, "messages": smsgs,
			})
			if err != nil {
				return err
			}
			if scode != 200 {
				return fmt.Errorf("chat save: %s", sout)
			}
			var saved struct {
				ID string `json:"id"`
			}
			json.Unmarshal(sout, &saved)
			fmt.Fprintf(os.Stderr, "saved chat %s\n", saved.ID)
		}
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
	outFile := fs.String("out", "", "write the prompts JSON to this file instead of stdout (F488)")
	stats := fs.Bool("stats", false, "aggregate the templates per category instead of listing them (F630)")
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
	if *stats { // F630: one row per category (template count); "(none)" groups uncategorized
		type pst struct {
			Category string `json:"category"`
			Prompts  int    `json:"prompts"`
		}
		byCat := map[string]*pst{}
		for _, p := range shown {
			cat := p.Category
			if cat == "" {
				cat = "(none)"
			}
			g := byCat[cat]
			if g == nil {
				g = &pst{Category: cat}
				byCat[cat] = g
			}
			g.Prompts++
		}
		names := make([]string, 0, len(byCat))
		for k := range byCat {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool {
			if byCat[names[i]].Prompts != byCat[names[j]].Prompts {
				return byCat[names[i]].Prompts > byCat[names[j]].Prompts
			}
			return names[i] < names[j]
		})
		rows := make([]pst, 0, len(names))
		for _, k := range names {
			rows = append(rows, *byCat[k])
		}
		if *jsonOut || *outFile != "" {
			b, _ := json.Marshal(rows)
			if *outFile != "" {
				if err := os.WriteFile(*outFile, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
				return nil
			}
			fmt.Println(string(b))
			return nil
		}
		if *csvOut {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"category", "prompts"})
			for _, r := range rows {
				_ = w.Write([]string{r.Category, strconv.Itoa(r.Prompts)})
			}
			w.Flush()
			return w.Error()
		}
		if len(rows) == 0 {
			fmt.Println("no prompt templates")
			return nil
		}
		fmt.Printf("%-24s %8s\n", "CATEGORY", "PROMPTS")
		for _, r := range rows {
			fmt.Printf("%-24s %8d\n", r.Category, r.Prompts)
		}
		return nil
	}
	if *jsonOut || *outFile != "" { // -out writes the same payload to a file (F488)
		payload := out
		if len(shown) != len(list) {
			b, err := json.Marshal(shown)
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
		stats := fs.Bool("stats", false, "aggregate the files per content type instead of listing them (F631)")
		fs.Parse(reorderFlags(rest))
		code, out, err := cc.Get("/api/v1/files")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("files returned %d: %s", code, out)
		}
		if *stats { // F631: one row per content type (files, bytes, last upload)
			var fl []struct {
				Entry struct {
					ContentType string    `json:"contentType"`
					Size        int64     `json:"size"`
					CreatedAt   time.Time `json:"created_at"`
				} `json:"entry"`
			}
			if err := json.Unmarshal(out, &fl); err != nil {
				return err
			}
			type fst struct {
				ContentType string `json:"content_type"`
				Files       int    `json:"files"`
				Bytes       int64  `json:"bytes"`
				Last        string `json:"last"`
			}
			byType := map[string]*fst{}
			lastT := map[string]time.Time{}
			for _, f := range fl {
				ct := f.Entry.ContentType
				if ct == "" {
					ct = "(none)"
				}
				g := byType[ct]
				if g == nil {
					g = &fst{ContentType: ct}
					byType[ct] = g
				}
				g.Files++
				g.Bytes += f.Entry.Size
				if f.Entry.CreatedAt.After(lastT[ct]) {
					lastT[ct] = f.Entry.CreatedAt
				}
			}
			names := make([]string, 0, len(byType))
			for k := range byType {
				names = append(names, k)
			}
			sort.Slice(names, func(i, j int) bool { return byType[names[i]].Bytes > byType[names[j]].Bytes })
			rows := make([]fst, 0, len(names))
			for _, k := range names {
				g := byType[k]
				if !lastT[k].IsZero() {
					g.Last = lastT[k].UTC().Format(time.RFC3339)
				}
				rows = append(rows, *g)
			}
			if *outFile != "" {
				b, _ := json.Marshal(rows)
				if err := os.WriteFile(*outFile, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(b))
				return nil
			}
			if *jsonOut {
				b, _ := json.Marshal(rows)
				fmt.Println(string(b))
				return nil
			}
			if *csvOut {
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"content_type", "files", "bytes", "last"})
				for _, r := range rows {
					_ = w.Write([]string{r.ContentType, strconv.Itoa(r.Files),
						strconv.FormatInt(r.Bytes, 10), r.Last})
				}
				w.Flush()
				return w.Error()
			}
			if len(rows) == 0 {
				fmt.Println("no uploaded files (upload via the web UI)")
				return nil
			}
			fmt.Printf("%-28s %6s %10s %-20s\n", "CONTENT TYPE", "FILES", "BYTES", "LAST (UTC)")
			for _, r := range rows {
				last := r.Last
				if t, err := time.Parse(time.RFC3339, r.Last); err == nil {
					last = t.Format("2006-01-02 15:04:05")
				}
				fmt.Printf("%-28s %6d %10s %-20s\n", r.ContentType, r.Files, fmtSize(r.Bytes), last)
			}
			return nil
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
		cfs := flag.NewFlagSet("files cat", flag.ExitOnError)
		cout := cfs.String("out", "", "write the stored bytes to this file instead of stdout (F613)")
		cfs.Parse(reorderFlags(rest))
		if cfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl files cat <id> [-out FILE]")
		}
		code, out, err := cc.Get("/api/v1/files/" + url.PathEscape(cfs.Arg(0)) + "/content")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("file %q not found (%d)", cfs.Arg(0), code)
		}
		if *cout != "" { // F613: download to disk, stdout stays clean
			if err := os.WriteFile(*cout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *cout, len(out))
			return nil
		}
		fmt.Print(string(out))
		return nil
	case "delete":
		fdfs := flag.NewFlagSet("files delete", flag.ExitOnError)
		fdjson := fdfs.Bool("json", false, "print the raw response JSON (F536)")
		fdfs.Parse(reorderFlags(rest))
		if fdfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl files delete <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/files/" + url.PathEscape(fdfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("delete returned %d: %s", code, out)
		}
		if *fdjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("deleted %s\n", fdfs.Arg(0))
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
		twfs := flag.NewFlagSet("settings test-webhook", flag.ExitOnError)
		twjson := twfs.Bool("json", false, "print the raw response JSON (F574)")
		twev := twfs.String("event", "", "event name to send (default \"test\"; F591)")
		twmsg := twfs.String("message", "", "message text to send (F591)")
		twfs.Parse(reorderFlags(args[1:]))
		body := map[string]any{}
		if *twev != "" {
			body["event"] = *twev
		}
		if *twmsg != "" {
			body["message"] = *twmsg
		}
		code, out, err := cc.Post("/api/v1/settings/notify-test", body)
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("notify-test returned %d: %s", code, out)
		}
		if *twjson {
			fmt.Print(string(out))
			return nil
		}
		var tr struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(out, &tr); err != nil {
			return err
		}
		if !tr.OK {
			return fmt.Errorf("webhook test failed: %s", tr.Error)
		}
		fmt.Println("test notification delivered to the configured webhook")
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
		sjson := fs.Bool("json", false, "print the raw updated settings JSON (F581)")
		fs.Parse(reorderFlags(args[1:]))
		body := map[string]any{}
		changed := []string{} // F581: echo which flags actually changed
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "json" {
				changed = append(changed, f.Name+"="+f.Value.String())
			}
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
		if *sjson {
			fmt.Print(string(out))
			return nil
		}
		sort.Strings(changed)
		fmt.Printf("settings updated: %s\n", strings.Join(changed, " "))
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
	csvOut := fs.Bool("csv", false, "output the batch results as CSV — prompt,ok,content,error (F605)")
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
	if *csvOut { // F605: one row per prompt, full (untruncated) text
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"prompt", "ok", "content", "error"})
		for _, r := range res.Results {
			_ = w.Write([]string{r.Prompt, strconv.FormatBool(r.Error == ""), r.Content, r.Error})
		}
		w.Flush()
		return w.Error()
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
		return fmt.Errorf("usage: llmctl key <generate|duplicate|show|note|tag|retag|enable|disable|rotate|list|revoke|prune|reset-usage>")
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
		outFile := fs.String("out", "", "write the raw creation JSON, including the secret, to this file instead of stdout (F507)")
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
		if *outFile != "" { // F507: raw JSON (with the secret) to a file, stdout stays clean
			if err := os.WriteFile(*outFile, out, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
			return nil
		}
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		fmt.Println("key:", r["key"])
		fmt.Println("name:", r["name"])
		fmt.Println("NOTE: shown only once — store it now.")
		return nil
	case "duplicate":
		// Create a fresh key carrying the same restrictions as an existing
		// one (F650); the secret is shown once, like `key generate`.
		if len(rest) == 0 {
			return fmt.Errorf("usage: llmctl key duplicate <id> [name] [-json]")
		}
		dfs := flag.NewFlagSet("key duplicate", flag.ExitOnError)
		djson := dfs.Bool("json", false, "print the raw creation JSON, including the secret (for scripts)")
		dfs.Parse(reorderFlags(rest[1:]))
		code, out, err := cc.Get("/api/v1/keys/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key duplicate: %s", out)
		}
		var src struct {
			Name               string     `json:"name"`
			TokenQuota         int64      `json:"tokenQuota"`
			AllowedModels      []string   `json:"allowedModels"`
			MaxConcurrent      int        `json:"maxConcurrent"`
			MaxRequests        int64      `json:"maxRequests"`
			RateRPS            float64    `json:"rateRPS"`
			RateBurst          int        `json:"rateBurst"`
			ExpiresAt          *time.Time `json:"expiresAt"`
			DailyCostBudgetUSD float64    `json:"dailyCostBudgetUSD"`
			Note               string     `json:"note"`
		}
		if err := json.Unmarshal(out, &src); err != nil {
			return err
		}
		name := strings.Join(dfs.Args(), " ")
		if strings.TrimSpace(name) == "" {
			name = src.Name + " copy"
		}
		body := map[string]any{
			"name":               name,
			"tokenQuota":         src.TokenQuota,
			"maxConcurrent":      src.MaxConcurrent,
			"maxRequests":        src.MaxRequests,
			"rateRPS":            src.RateRPS,
			"rateBurst":          src.RateBurst,
			"dailyCostBudgetUSD": src.DailyCostBudgetUSD,
			"note":               src.Note,
		}
		if len(src.AllowedModels) > 0 {
			body["allowedModels"] = src.AllowedModels
		}
		if src.ExpiresAt != nil {
			if d := time.Until(*src.ExpiresAt); d > 0 {
				body["expiresInDays"] = int((d + 23*time.Hour) / (24 * time.Hour)) // round up to whole days
			}
		}
		code, out, err = cc.Post("/api/v1/keys", body)
		if err != nil {
			return err
		}
		if code != 201 {
			return fmt.Errorf("key duplicate returned %d: %s", code, out)
		}
		if *djson {
			fmt.Print(string(out))
			return nil
		}
		var r map[string]any
		json.Unmarshal(out, &r)
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
		kout := kfs.String("out", "", "write the raw key JSON to this file instead of stdout (F506)")
		kfs.Parse(reorderFlags(rest[1:]))
		code, out, err := cc.Get("/api/v1/keys/" + url.PathEscape(rest[0]))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key show returned %d: %s", code, out)
		}
		if *kout != "" { // F506: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*kout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *kout, len(out))
			return nil
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
			Tags               []string   `json:"tags"` // free-form labels (F686)
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
		if len(kv.Tags) > 0 { // tags line only when present (F686)
			fmt.Printf("tags:     %s\n", strings.Join(kv.Tags, ", "))
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
		kujjson := ufs.Bool("json", false, "print the raw response JSON (F539)")
		ufs.Parse(reorderFlags(rest))
		if ufs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key update <id> [-name X] [-note X] [-models a,b] [-quota N] [-maxreq N] [-rps X] [-burst N] [-expires D] [-budget X] [-max-concurrent N] [-json]")
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
		if *kujjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("updated key %s\n", ufs.Arg(0))
		return nil
	case "note":
		knfs := flag.NewFlagSet("key note", flag.ExitOnError)
		knjson := knfs.Bool("json", false, "print the raw response JSON (F528)")
		knfs.Parse(reorderFlags(rest))
		if knfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key note <id> <text...> [-json]  (empty text clears)")
		}
		note := strings.TrimSpace(strings.Join(knfs.Args()[1:], " "))
		code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(knfs.Arg(0)), map[string]any{"note": note})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key note returned %d: %s", code, out)
		}
		if *knjson {
			fmt.Print(string(out))
			return nil
		}
		if note == "" {
			fmt.Println("note cleared")
		} else {
			fmt.Printf("note set: %s\n", note)
		}
		return nil
	case "tag":
		// Replace a key's tags (comma-separated); empty clears (F680).
		tgfs := flag.NewFlagSet("key tag", flag.ExitOnError)
		tgjson := tgfs.Bool("json", false, "print the raw response JSON")
		tgfs.Parse(reorderFlags(rest))
		if tgfs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl key tag <id> [a,b,c] [-json]  (empty clears)")
		}
		var tags []string
		if tgfs.NArg() > 1 {
			for _, t := range strings.Split(tgfs.Arg(1), ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
		code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(tgfs.Arg(0))+"/tags",
			map[string]any{"tags": tags})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key tag returned %d: %s", code, out)
		}
		if *tgjson {
			fmt.Print(string(out))
			return nil
		}
		if len(tags) == 0 {
			fmt.Printf("key %s tags cleared\n", tgfs.Arg(0))
		} else {
			fmt.Printf("key %s tags: %s\n", tgfs.Arg(0), strings.Join(tags, ", "))
		}
		return nil
	case "retag":
		// Bulk-rename or drop a tag across all keys (F680), twin of `models retag`.
		rtfs := flag.NewFlagSet("key retag", flag.ExitOnError)
		rtrm := rtfs.Bool("rm", false, "drop the tag instead of renaming it")
		rtjson := rtfs.Bool("json", false, "print the count as JSON")
		rtfs.Parse(reorderFlags(rest))
		if rtfs.NArg() < 1 || (!*rtrm && rtfs.NArg() < 2) {
			return fmt.Errorf("usage: llmctl key retag <old-tag> <new-tag> | key retag -rm <tag> [-json]")
		}
		oldTag, newTag := rtfs.Arg(0), rtfs.Arg(1)
		code, out, err := cc.Get("/api/v1/keys")
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("keys returned %d: %s", code, out)
		}
		var ks []struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(out, &ks); err != nil {
			return err
		}
		changed := 0
		for _, k := range ks {
			found := false
			var tags []string
			for _, t := range k.Tags {
				if t != oldTag {
					tags = append(tags, t)
					continue
				}
				found = true
				if !*rtrm {
					tags = append(tags, newTag)
				}
			}
			if !found {
				continue
			}
			code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(k.ID)+"/tags",
				map[string]any{"tags": tags})
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("key retag on %s: %s", k.ID, out)
			}
			changed++
		}
		if *rtjson {
			fmt.Printf("{\"changed\": %d}\n", changed)
			return nil
		}
		if *rtrm {
			fmt.Printf("removed tag %q from %d key(s)\n", oldTag, changed)
		} else {
			fmt.Printf("renamed tag %q -> %q on %d key(s)\n", oldTag, newTag, changed)
		}
		return nil
	case "disable", "enable":
		keds := flag.NewFlagSet("key "+sub, flag.ExitOnError)
		kejson := keds.Bool("json", false, "print the raw response JSON (F535)")
		keds.Parse(reorderFlags(rest))
		if keds.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key %s <id> [-json]", sub)
		}
		disabled := sub == "disable"
		code, out, err := cc.Do("PATCH", "/api/v1/keys/"+url.PathEscape(keds.Arg(0)), map[string]any{"disabled": disabled})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("key %s returned %d: %s", sub, code, out)
		}
		if *kejson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("key %s %sd\n", keds.Arg(0), sub)
		return nil
	case "list":
		fs := flag.NewFlagSet("key list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw keys JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the key table as CSV (F374)")
		outFile := fs.String("out", "", "write the raw keys JSON to this file instead of stdout (F481)")
		tagF := fs.String("tag", "", "only keys carrying this tag (F680)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/keys")
		if err != nil {
			return err
		}
		if *tagF != "" { // F680: exact tag filter; filter the raw JSON so -json keeps every field
			var raw []map[string]any
			if err := json.Unmarshal(out, &raw); err != nil {
				return err
			}
			keep := raw[:0]
			for _, k := range raw {
				ts, _ := k["tags"].([]any)
				for _, t := range ts {
					if s, _ := t.(string); s == *tagF {
						keep = append(keep, k)
						break
					}
				}
			}
			if out, err = json.Marshal(keep); err != nil {
				return err
			}
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
			Tags               []string   `json:"tags"` // free-form labels (F680)
			Disabled           bool       `json:"disabled"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return err
		}
		if len(list) == 0 {
			if *csvOut { // keep the stream parseable (F374)
				w := csv.NewWriter(os.Stdout)
				_ = w.Write([]string{"id", "name", "prefix", "disabled", "expires_at", "last_used", "used_tokens", "token_quota", "used_requests", "max_requests", "used_cost_usd", "today_cost_usd", "daily_cost_budget_usd", "note", "tags"})
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
			_ = w.Write([]string{"id", "name", "prefix", "disabled", "expires_at", "last_used", "used_tokens", "token_quota", "used_requests", "max_requests", "used_cost_usd", "today_cost_usd", "daily_cost_budget_usd", "note", "tags"})
			for _, k := range list {
				_ = w.Write([]string{k.ID, k.Name, k.Prefix, strconv.FormatBool(k.Disabled),
					rfc(k.ExpiresAt), rfc(k.LastUsed),
					strconv.FormatInt(k.UsedTok, 10), strconv.FormatInt(k.TokenQuota, 10),
					strconv.FormatInt(k.UsedReqs, 10), strconv.FormatInt(k.MaxRequests, 10),
					strconv.FormatFloat(k.UsedCost, 'f', 4, 64), strconv.FormatFloat(k.TodayCost, 'f', 4, 64),
					strconv.FormatFloat(k.DailyCostBudgetUSD, 'f', 4, 64), k.Note, strings.Join(k.Tags, ";")})
			}
			w.Flush()
			return w.Error()
		}
		fmt.Printf("%-17s %-14s %-10s %10s %9s %8s %-12s %-12s %-9s %-20s %s\n", "ID", "NAME", "PREFIX", "TOKENS", "COST $", "REQS", "EXPIRES", "LAST USED", "QUOTA", "TAGS", "NOTE")
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
			fmt.Printf("%-17s %-14s %-10s %10d %9.4f %8d %-12s %-12s %-9s %-20s %s\n",
				k.ID, k.Name, k.Prefix+"…", k.UsedTok, k.UsedCost, k.UsedReqs, expires, last, quota, strings.Join(k.Tags, ","), note)
		}
		return nil
	case "revoke":
		rfs := flag.NewFlagSet("key revoke", flag.ExitOnError)
		rjson := rfs.Bool("json", false, "print the raw response JSON (F565)")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key revoke <id> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/keys/" + url.PathEscape(rfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("revoke returned %d: %s", code, out)
		}
		if *rjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("key %s revoked\n", rfs.Arg(0))
		return nil
	case "prune":
		pfs := flag.NewFlagSet("key prune", flag.ExitOnError)
		dry := pfs.Bool("dry-run", false, "list expired keys without deleting them")
		pout := pfs.String("out", "", "write the raw prune-result JSON to this file instead of stdout (F509)")
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
		if *pout != "" { // F509: raw JSON to a file, stdout stays clean
			if err := os.WriteFile(*pout, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *pout, len(out))
			return nil
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
		ruFS := flag.NewFlagSet("key reset-usage", flag.ExitOnError)
		rujson := ruFS.Bool("json", false, "print the raw response JSON (F572)")
		ruFS.Parse(reorderFlags(rest))
		if ruFS.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key reset-usage <id> [-json]")
		}
		code, out, err := cc.Post("/api/v1/keys/"+url.PathEscape(ruFS.Arg(0))+"/reset-usage", map[string]any{})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("reset-usage returned %d: %s", code, out)
		}
		if *rujson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("usage counters reset for key %s\n", ruFS.Arg(0))
		return nil
	case "rotate":
		rfs := flag.NewFlagSet("key rotate", flag.ExitOnError)
		rjson := rfs.Bool("json", false, "print the raw rotation JSON, including the new secret (for scripts)")
		rout := rfs.String("out", "", "write the raw rotation JSON, including the new secret, to this file instead of stdout (F508)")
		rfs.Parse(reorderFlags(rest))
		if rfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl key rotate <id> [-json] [-out FILE]")
		}
		code, out, err := cc.Post("/api/v1/keys/"+rfs.Arg(0)+"/rotate", map[string]any{})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("rotate returned %d: %s", code, out)
		}
		if *rout != "" { // F508: raw JSON (with the new secret) to a file, stdout stays clean
			if err := os.WriteFile(*rout, out, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *rout, len(out))
			return nil
		}
		if *rjson {
			fmt.Print(string(out))
			return nil
		}
		var rot struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Prefix string `json:"prefix"`
		}
		if err := json.Unmarshal(out, &rot); err != nil {
			return err
		}
		fmt.Printf("key %s rotated — new secret: %s\n", rot.ID, rot.Key)
		fmt.Println("store it now; it is shown only this once")
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
		aifs := flag.NewFlagSet("autostart import", flag.ExitOnError)
		aijson := aifs.Bool("json", false, "print {\"created\",\"skipped\"} as JSON (F542)")
		aifs.Parse(reorderFlags(rest))
		if aifs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl autostart import <file.json> [-json]")
		}
		data, err := os.ReadFile(aifs.Arg(0))
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
				if *aijson {
					fmt.Fprintf(os.Stderr, "  skip %v: %s\n", s["modelId"], out)
				} else {
					fmt.Printf("  skip %v: %s\n", s["modelId"], out)
				}
				skipped++
				continue
			}
			created++
		}
		if *aijson {
			b, _ := json.Marshal(map[string]int{"created": created, "skipped": skipped})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("imported %d auto-start entries (%d skipped)\n", created, skipped)
		return nil
	case "list":
		fs := flag.NewFlagSet("autostart list", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "print the raw autostart JSON (for scripts)")
		csvOut := fs.Bool("csv", false, "output the autostart list as CSV (F390)")
		outFile := fs.String("out", "", "write the raw autostart JSON to this file instead of stdout (F494)")
		fs.Parse(reorderFlags(rest))
		_, out, err := cc.Get("/api/v1/autostart")
		if err != nil {
			return err
		}
		if *outFile != "" { // F494: raw JSON to a file, stdout stays clean
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
		jsonOut := fs.Bool("json", false, "print the raw response JSON (F526)")
		var vars stringList
		fs.Var(&vars, "var", "parameter k=v (repeatable)")
		fs.Parse(reorderFlags(rest))
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl autostart add <model> [--backend id] [--instance name] [--port N] [--var k=v ...] [-json]")
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
		if *jsonOut {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("auto-start enabled for %q (relaunches when the daemon starts)\n", fs.Arg(0))
		return nil
	case "remove":
		rmfs := flag.NewFlagSet("autostart remove", flag.ExitOnError)
		rmjson := rmfs.Bool("json", false, "print the raw response JSON (F527)")
		rmfs.Parse(reorderFlags(rest))
		if rmfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl autostart remove <model> [-json]")
		}
		code, out, err := cc.Delete("/api/v1/autostart/" + urlEscape(rmfs.Arg(0)))
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("remove failed: %s", string(out))
		}
		if *rmjson {
			fmt.Print(string(out))
			return nil
		}
		fmt.Printf("auto-start removed for %q\n", rmfs.Arg(0))
		return nil
	default:
		return fmt.Errorf("unknown autostart subcommand: %s", sub)
	}
}

// kbContextFor searches a knowledge base for the query and builds the
// context block shared by the one-shot -kb flag and the REPL /kb command (F342).
// Returns "" when nothing matches.
// fileContextFor reads a local file for `chat -file`, formatted like the
// Chat UI's /file injection (F669). Capped at 1 MiB.
func fileContextFor(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	if len(b) > 1<<20 {
		return "", fmt.Errorf("file too large (max 1 MiB): %s", path)
	}
	return fmt.Sprintf("File: %s\n\n%s", filepath.Base(path), string(b)), nil
}

func kbContextFor(cc *daemon.ControlClient, kbIDs, query string) (string, error) {
	// kbIDs may be a comma-separated list: search each KB and merge the
	// top hits by score (F666), same as the scheduler's -kb.
	var names []string
	var hits []struct {
		Filename  string  `json:"filename"`
		Score     float64 `json:"score"`
		ChunkText string  `json:"chunk_text"`
	}
	for _, id := range strings.Split(kbIDs, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		code, out, err := cc.Post("/api/v1/knowledge-bases/"+url.PathEscape(id)+"/search",
			map[string]any{"q": query, "limit": 3})
		if err != nil {
			return "", err
		}
		if code != 200 {
			return "", fmt.Errorf("kb search: %s", out)
		}
		var res []struct {
			Filename  string  `json:"filename"`
			Score     float64 `json:"score"`
			ChunkText string  `json:"chunk_text"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return "", err
		}
		names = append(names, id)
		hits = append(hits, res...)
	}
	if len(hits) == 0 {
		return "", nil
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > 3 {
		hits = hits[:3]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Relevant context from knowledge base %q:\n\n", strings.Join(names, ", "))
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
	tagsFlag := fs.String("tags", "", "with -save: comma-separated tags for the new chat (F687)")
	contChat := fs.String("chat", "", "continue this saved chat — its history is sent and the reply appended (one-shot and REPL; F297, REPL F685)")
	contLast := fs.Bool("continue", false, "with -prompt: continue the most recently updated saved chat (F312; REPL too, F685)")
	kbFlag := fs.String("kb", "", "prepend the top knowledge-base matches as context (one-shot and REPL; comma-separated ids search all and merge, like the Chat UI; F302, F666, F667)")
	fileFlag := fs.String("file", "", "prepend the contents of this local file as a context message (one-shot and REPL; like the Chat UI /file, F669)")
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
		var contTags []string // kept so continuing a chat doesn't wipe its tags (F687)
		if *contChat != "" {
			code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(*contChat))
			if err != nil {
				return err
			}
			if code != 200 {
				return fmt.Errorf("chat %s: %s", *contChat, out)
			}
			var ch struct {
				Title    string   `json:"title"`
				Tags     []string `json:"tags"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(out, &ch); err != nil {
				return err
			}
			contTitle, contTags = ch.Title, ch.Tags
			for _, m := range ch.Messages {
				hist = append(hist, map[string]any{"role": m.Role, "content": m.Content})
				contMsgs = append(contMsgs, map[string]string{"role": m.Role, "content": m.Content})
			}
		}
		if strings.TrimSpace(*system) != "" && len(contMsgs) == 0 {
			hist = append(hist, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
		}
		if *fileFlag != "" { // file context first, like the Chat UI (F669)
			ctxMsg, err := fileContextFor(*fileFlag)
			if err != nil {
				return err
			}
			hist = append(hist, map[string]any{"role": "user", "content": ctxMsg})
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
				"tags":     contTags, // don't wipe the chat's tags on continue (F687)
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
			body := map[string]any{
				"title":    *saveChat,
				"model":    model,
				"messages": msgs,
			}
			if *tagsFlag != "" { // tag the chat at creation (F687)
				tagList := []string{}
				for _, t := range strings.Split(*tagsFlag, ",") {
					if t = strings.TrimSpace(t); t != "" {
						tagList = append(tagList, t)
					}
				}
				body["tags"] = tagList
			}
			code, out, err := cc.Post("/api/v1/chats", body)
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
	replChat := ""        // id of the chat created by /save; /title and /tag act on it (F683)
	replTitle := ""       // its title, kept in sync on /save and -chat load (F685)
	var replTags []string // its tags, kept across auto-appends (F687)
	replAuto := false     // -chat/-continue: append each exchange back to replChat (F685)
	if strings.TrimSpace(*system) != "" {
		history = append(history, map[string]any{"role": "system", "content": strings.TrimSpace(*system)})
	}
	kbID := strings.TrimSpace(*kbFlag) // -kb seeds the REPL too (F667); /kb can change it
	fileCtx := ""                      // -file seeds the REPL too (F669)
	if *fileFlag != "" {
		var ferr error
		fileCtx, ferr = fileContextFor(*fileFlag)
		if ferr != nil {
			return ferr
		}
	}
	// -chat/-continue seed the REPL too: load the saved history and append
	// every exchange back to that chat as the conversation grows (F685).
	if *contLast && *contChat == "" {
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
		fmt.Printf("continuing chat %s\n", *contChat)
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
			Title    string   `json:"title"`
			Tags     []string `json:"tags"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(out, &ch); err != nil {
			return err
		}
		for _, m := range ch.Messages {
			history = append(history, map[string]any{"role": m.Role, "content": m.Content})
		}
		replChat, replTitle, replAuto = *contChat, ch.Title, true
		replTags = ch.Tags
		fmt.Printf("[loaded %d message(s) from chat %s — replies are appended to it]\n", len(ch.Messages), replChat)
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
		// REPL slash commands (F331).
		regen := false // /regenerate falls through to the send path (F684)
		if strings.HasPrefix(line, "/") {
			fields := strings.Fields(line)
			cmd := fields[0]
			rest := strings.TrimSpace(strings.TrimPrefix(line, cmd))
			switch cmd {
			case "/help":
				fmt.Println("commands: /help /clear /system TEXT /model ID /kb ID[,ID…] /kb - /save [title] /title TEXT /tag [a,b|-] /regenerate /quit")
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
				// Validate every comma-separated id against the KB list
				// before attaching (F667).
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
				var ids []string
				bad := ""
				for _, id := range strings.Split(rest, ",") {
					id = strings.TrimSpace(id)
					if id == "" {
						continue
					}
					found := false
					for _, k := range kbs {
						if k.ID == id {
							found = true
						}
					}
					if !found {
						bad = id
						break
					}
					ids = append(ids, id)
				}
				if bad != "" || len(ids) == 0 {
					if bad == "" {
						bad = rest
					}
					fmt.Printf("[no knowledge base %q — see llmctl kb list]\n", bad)
					continue
				}
				kbID = strings.Join(ids, ",")
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
				replChat, replTitle = saved.ID, title // /title and /tag can act on it now (F683)
				fmt.Printf("[saved as chat %s: %q]\n", saved.ID, title)
			case "/title":
				// Rename the chat saved by /save (twin of `chats rename`, F683).
				if replChat == "" {
					fmt.Println("[no saved chat yet — use /save first]")
					continue
				}
				if rest == "" {
					fmt.Println("[usage: /title NEW TITLE]")
					continue
				}
				code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(replChat))
				if err != nil || code != 200 {
					fmt.Printf("[rename failed: %v %s]\n", err, out)
					continue
				}
				var conv map[string]any
				if err := json.Unmarshal(out, &conv); err != nil {
					fmt.Printf("[rename failed: %v]\n", err)
					continue
				}
				conv["title"] = rest
				code, out, err = cc.Put("/api/v1/chats/"+url.PathEscape(replChat), conv)
				if err != nil || code != 200 {
					fmt.Printf("[rename failed: %v %s]\n", err, out)
					continue
				}
				fmt.Printf("[chat %s renamed to %q]\n", replChat, rest)
			case "/tag":
				// Tag the chat saved by /save (twin of `chats tag`, F683);
				// no argument shows the current tags, "-" clears them.
				if replChat == "" {
					fmt.Println("[no saved chat yet — use /save first]")
					continue
				}
				if rest == "" {
					code, out, err := cc.Get("/api/v1/chats/" + url.PathEscape(replChat))
					if err != nil || code != 200 {
						fmt.Printf("[tag lookup failed: %v %s]\n", err, out)
						continue
					}
					var cur struct {
						Tags []string `json:"tags"`
					}
					json.Unmarshal(out, &cur)
					if len(cur.Tags) == 0 {
						fmt.Println("[no tags — usage: /tag a,b  or  /tag - to clear]")
					} else {
						fmt.Printf("[tags: %s]\n", strings.Join(cur.Tags, ", "))
					}
					continue
				}
				var tags []string
				if rest != "-" {
					for _, t := range strings.Split(rest, ",") {
						if t = strings.TrimSpace(t); t != "" {
							tags = append(tags, t)
						}
					}
				}
				code, out, err := cc.Do("PATCH", "/api/v1/chats/"+url.PathEscape(replChat)+"/tags",
					map[string]any{"tags": tags})
				if err != nil || code != 200 {
					fmt.Printf("[tag failed: %v %s]\n", err, out)
					continue
				}
				if len(tags) == 0 {
					fmt.Println("[tags cleared]")
				} else {
					fmt.Printf("[tags set: %s]\n", strings.Join(tags, ", "))
				}
				replTags = tags // keep auto-appends from wiping them (F687)
			case "/regenerate":
				// Drop the last assistant reply and re-send the last user
				// message — handy after /system or /model tweaks (F684).
				if len(history) == 0 {
					fmt.Println("[nothing to regenerate — ask something first]")
					continue
				}
				if fmt.Sprint(history[len(history)-1]["role"]) == "assistant" {
					history = history[:len(history)-1]
				}
				if len(history) == 0 || fmt.Sprint(history[len(history)-1]["role"]) != "user" {
					fmt.Println("[nothing to regenerate — ask something first]")
					continue
				}
				line = fmt.Sprint(history[len(history)-1]["content"])
				regen = true
			default:
				fmt.Printf("[unknown command %q — try /help]\n", cmd)
			}
			if !regen {
				continue
			}
		}

		// Append user message (skipped when /regenerate re-sends the last one).
		if !regen {
			history = append(history, map[string]any{"role": "user", "content": line})
		}

		// With -file and/or a /kb attached, prepend their context turns for
		// this message (same shape as the one-shot flags, F342, F669).
		reqMsgs := history
		var ctxMsgs []string
		if fileCtx != "" {
			ctxMsgs = append(ctxMsgs, fileCtx)
		}
		if kbID != "" {
			ctxMsg, kerr := kbContextFor(cc, kbID, line)
			if kerr != nil {
				fmt.Printf("[kb search failed: %v]\n", kerr)
			} else if ctxMsg != "" {
				ctxMsgs = append(ctxMsgs, ctxMsg)
			}
		}
		if len(ctxMsgs) > 0 {
			reqMsgs = make([]map[string]any, 0, len(history)+len(ctxMsgs))
			reqMsgs = append(reqMsgs, history[:len(history)-1]...)
			for _, c := range ctxMsgs {
				reqMsgs = append(reqMsgs, map[string]any{"role": "user", "content": c})
			}
			reqMsgs = append(reqMsgs, history[len(history)-1])
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
		if replAuto && acc.Len() > 0 { // keep the continued chat in sync (F685)
			msgs := []map[string]string{}
			for _, h := range history {
				if fmt.Sprint(h["role"]) != "system" {
					msgs = append(msgs, map[string]string{"role": fmt.Sprint(h["role"]), "content": fmt.Sprint(h["content"])})
				}
			}
			code, out, err := cc.Put("/api/v1/chats/"+url.PathEscape(replChat), map[string]any{
				"title": replTitle, "model": model, "messages": msgs, "tags": replTags,
			})
			if err != nil || code != 200 {
				fmt.Printf("[chat append failed: %v %s]\n", err, out)
			}
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
		vfs := flag.NewFlagSet("backup verify", flag.ExitOnError)
		vjson := vfs.Bool("json", false, "print the verify result as JSON (F545)")
		vfs.Parse(reorderFlags(args[1:]))
		if vfs.NArg() == 0 {
			return fmt.Errorf("usage: llmctl backup verify <file.tar.gz> [-json]")
		}
		entries, err := backup.Verify(vfs.Arg(0))
		if err != nil {
			return err
		}
		if *vjson {
			b, _ := json.Marshal(map[string]any{"file": vfs.Arg(0), "entries": entries, "ok": true})
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("backup %s ok — %d entries, state.json valid\n", vfs.Arg(0), entries)
		return nil
	}
	bfs := flag.NewFlagSet("backup", flag.ExitOnError)
	bjson := bfs.Bool("json", false, "print the backup result as JSON (F545)")
	bfs.Parse(reorderFlags(args))
	out := bfs.Arg(0)
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
	if *bjson {
		b, _ := json.Marshal(map[string]any{"file": out, "bytes": st.Size()})
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("backup written to %s (%s)\n", out, fmtSize(st.Size()))
	return nil
}

// cmdRestore imports a tar.gz backup into the llmctl state directory.
func cmdRestore(args []string) error {
	rfs := flag.NewFlagSet("restore", flag.ExitOnError)
	rjson := rfs.Bool("json", false, "print the restore result as JSON (F545)")
	rfs.Parse(reorderFlags(args))
	if rfs.NArg() == 0 {
		return fmt.Errorf("usage: llmctl restore <file.tar.gz> [-json]")
	}
	f, err := os.Open(rfs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := backup.Restore(f)
	if err != nil {
		return err
	}
	if *rjson {
		b, _ := json.Marshal(map[string]any{"restored": n, "home": paths.Home()})
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("restored %d file(s) into %s\n", n, paths.Home())
	return nil
}

// cmdApi is a raw passthrough to the management API (F547), an escape hatch
// for scripting and testing endpoints without a dedicated command:
//
//	llmctl api GET /api/v1/status
//	llmctl api POST /api/v1/keys -d '{"name":"ci"}'
//
// The response body is printed verbatim; a non-2xx status exits non-zero.
// cmdOpen launches the system browser at the daemon's web UI (F577).
// -print just prints the URL (handy over ssh or on headless hosts).
func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	printOnly := fs.Bool("print", false, "print the UI URL instead of launching a browser")
	fs.Parse(reorderFlags(args))
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	ui := cc.Base
	if fs.NArg() > 0 { // F598: deep-link to a UI page, e.g. llmctl open requests
		ui = strings.TrimSuffix(ui, "/") + "/" + strings.Trim(fs.Arg(0), "/")
	}
	if *printOnly {
		fmt.Println(ui)
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", ui)
	case "darwin":
		cmd = exec.Command("open", ui)
	default:
		cmd = exec.Command("xdg-open", ui)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("no browser available (%v) — open %s yourself", err, ui)
	}
	fmt.Printf("opened %s in your browser\n", ui)
	return nil
}

func cmdApi(args []string) error {
	fs := flag.NewFlagSet("api", flag.ExitOnError)
	data := fs.String("d", "", "request body (JSON string)")
	dataFile := fs.String("d-file", "", "read the request body from this file (- = stdin)")
	outFile := fs.String("out", "", "write the response body to this file instead of stdout")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 2 {
		return fmt.Errorf("usage: llmctl api <GET|POST|PUT|PATCH|DELETE> /path [-d JSON | -d-file F] [-out FILE]")
	}
	method := strings.ToUpper(fs.Arg(0))
	path := fs.Arg(1)
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with / (e.g. /api/v1/status)")
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return fmt.Errorf("unsupported method: %s (use GET, POST, PUT, PATCH or DELETE)", method)
	}
	raw := *data
	if *dataFile != "" {
		if *dataFile == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			raw = string(b)
		} else {
			b, err := os.ReadFile(*dataFile)
			if err != nil {
				return err
			}
			raw = string(b)
		}
	}
	var body any
	if strings.TrimSpace(raw) != "" {
		if !json.Valid([]byte(raw)) {
			return fmt.Errorf("-d/-d-file body is not valid JSON")
		}
		body = json.RawMessage(raw)
	}
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Do(method, path, body)
	if err != nil {
		return err
	}
	if *outFile != "" {
		if werr := os.WriteFile(*outFile, out, 0o644); werr != nil {
			return werr
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *outFile, len(out))
	} else if len(out) > 0 {
		fmt.Println(strings.TrimRight(string(out), "\n"))
	}
	if code/100 != 2 {
		return fmt.Errorf("api %s %s returned %d", method, path, code)
	}
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
