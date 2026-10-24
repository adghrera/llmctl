// Command llmctl is the CLI for the llmctl daemon. It is a thin client over
// the management API: `llmctl up` runs the daemon (gateway + API + UI on one
// port); every other command talks to the running daemon.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"llmctl/internal/daemon"
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
	case "key":
		err = cmdKey(args)
	case "autostart":
		err = cmdAutoStart(args)
	case "chat":
		err = cmdChat(args)
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
  llmctl status
  llmctl ps
  llmctl backends list
  llmctl backends install <id> [--asset <hint>]
  llmctl backends uninstall <id>
  llmctl backends add <plugin.json>
  llmctl backends remove <id>
  llmctl models list
  llmctl models install <repo-or-url> [--file f.gguf] [--rev main] [--backend llama.cpp] [--id ID]
  llmctl models remove <id>
  llmctl models ctx <model-id> <tokens|0>
  llmctl models hf-search <query>
  llmctl models hf-files <repo>
  llmctl start <model> [--backend ID] [--instance NAME] [--var k=v ...]
  llmctl stop <instance> [--force]
  llmctl key generate [name]
  llmctl key list
  llmctl key revoke <id>
  llmctl autostart list
  llmctl autostart add <model> [--backend ID] [--instance NAME] [--port N] [--var k=v ...]
  llmctl autostart remove <model>
  llmctl chat [--model ID] [--key KEY]
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
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	code, out, err := cc.Get("/api/v1/status")
	if err != nil {
		return err
	}
	var st map[string]any
	json.Unmarshal(out, &st)
	fmt.Printf("daemon: running (%s)\n", cc.Base)
	fmt.Printf("backends: %v installed\n", st["backends"])
	fmt.Printf("models: %v downloaded\n", st["models"])
	fmt.Printf("instances: %v\n", st["instances"])
	fmt.Printf("gateway: %v\n", st["gateway"])
	_ = code
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
	var list []map[string]any
	json.Unmarshal(out, &list)
	if len(list) == 0 {
		fmt.Println("no instances (start one: llmctl start <model-id>)")
		return nil
	}
	fmt.Printf("%-28s %-24s %-12s %-8s %-7s %s\n", "INSTANCE", "MODEL", "BACKEND", "TRANS", "PORT", "STATUS")
	for _, i := range list {
		fmt.Printf("%-28s %-24s %-12s %-8s %-7v %s\n",
			i["id"], i["modelId"], i["backendId"], i["transport"], i["port"], i["status"])
	}
	return nil
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
		return fmt.Errorf("usage: llmctl models <list|install|remove|hf-search|hf-files>")
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
		var list []map[string]any
		json.Unmarshal(out, &list)
		if len(list) == 0 {
			fmt.Println("no models downloaded")
			return nil
		}
		fmt.Printf("%-40s %-12s %-10s %-9s %s\n", "ID", "BACKEND", "SIZE", "CTX", "RUNNING")
		for _, m := range list {
			ctxv := "-"
			if v, ok := m["ctxSize"].(float64); ok && v > 0 {
				ctxv = fmt.Sprintf("%d", int(v))
			}
			fmt.Printf("%-40s %-12s %-10v %-9s %v\n", m["id"], m["backendId"], m["sizeBytes"], ctxv, m["running"])
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
			return fmt.Errorf("usage: llmctl models remove <id>")
		}
		code, out, err := cc.Delete("/api/v1/models/" + rest[0])
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

func cmdKey(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl key <generate|list|revoke>")
	}
	sub, rest := args[0], args[1:]
	cc, err := daemon.NewControlClient()
	if err != nil {
		return err
	}
	switch sub {
	case "generate":
		name := ""
		if len(rest) > 0 {
			name = rest[0]
		}
		code, out, err := cc.Post("/api/v1/keys", map[string]any{"name": name})
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
	case "list":
		_, out, err := cc.Get("/api/v1/keys")
		if err != nil {
			return err
		}
		var list []map[string]any
		json.Unmarshal(out, &list)
		if len(list) == 0 {
			fmt.Println("no keys")
			return nil
		}
		for _, k := range list {
			fmt.Printf("%s  %s…  (%s)\n", k["id"], k["prefix"], k["name"])
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
