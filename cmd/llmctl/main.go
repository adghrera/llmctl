// llmctl — multi-backend local LLM manager with an OpenAI-compatible gateway.
//
//	 llmctl backends list|install|remove
//	 llmctl models list|available|install|remove
//	 llmctl up|down|status            (daemon lifecycle)
//	 llmctl start <model> | stop <id> | ps
//	 llmctl serve                     (gateway in foreground)
//	 llmctl key generate
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"llmctl/internal/daemon"
	"llmctl/internal/downloader"
	"llmctl/internal/installer"
	"llmctl/internal/manifest"
	"llmctl/internal/paths"
	"llmctl/internal/proxy"
	"llmctl/internal/store"
	"llmctl/internal/supervisor"
)

const version = "0.1.0"

func main() {
	log := logPrinter()
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
	case "serve":
		err = cmdServe(args)
	case "status":
		err = cmdStatus(args)
	case "ps":
		err = cmdPS(args)
	case "start":
		err = cmdStart(args)
	case "stop":
		err = cmdStop(args)
	case "backends":
		err = cmdBackends(args)
	case "models":
		err = cmdModels(args)
	case "key":
		err = cmdKey(args)
	case "version":
		fmt.Println("llmctl", version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log("error: %v", err)
		os.Exit(1)
	}
}

func logPrinter() func(string, ...any) {
	return func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
}

func usage() {
	fmt.Println(`llmctl ` + version + ` — local multi-backend LLM manager with OpenAI-compatible API

USAGE
  llmctl up [--addr :8080] [--control 127.0.0.1:8081] [--api-key sk-...]
                            start the daemon (gateway + supervisor) in foreground
  llmctl down               stop the daemon and all owned instances
  llmctl status             daemon + instance overview
  llmctl ps                 list running instances
  llmctl start <model-id> [--instance name] [--port N] [--var k=v ...]
                            launch a model (routes through the daemon)
  llmctl stop <instance> [--force]
  llmctl backends list
  llmctl backends install <backend-id> [--asset hint]
  llmctl backends remove <backend-id>
  llmctl models list        installed models
  llmctl models available   curated registry
  llmctl models install <model-id> | --hf <repo> [--file name]
  llmctl models remove <model-id>
  llmctl key generate       print a new gateway API key
  llmctl serve              gateway only (advanced; no supervisor)

API (OpenAI-compatible):
  POST http://127.0.0.1:8080/v1/chat/completions
  GET  http://127.0.0.1:8080/v1/models
  POST http://127.0.0.1:8080/v1/completions
  POST http://127.0.0.1:8080/v1/embeddings`)
}

// ---- daemon lifecycle ----

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "gateway listen address")
	control := fs.String("control", "127.0.0.1:8081", "control API (localhost only)")
	rps := fs.Float64("rate", 20, "sustained requests/sec per client")
	burst := fs.Int("burst", 60, "per-client burst size")
	maxInf := fs.Int("max-inflight", 512, "max concurrent in-flight requests")
	var keys stringList
	fs.Var(&keys, "api-key", "required API key (repeatable; empty = open)")
	fs.Parse(args)

	return daemon.Run(context.Background(), daemon.Options{
		GatewayAddr: *addr, ControlAddr: *control,
		APIKeys: keys, RateRPS: *rps, RateBurst: *burst, MaxInflight: *maxInf,
	})
}

func cmdDown(args []string) error {
	d := daemon.ReadDaemonFile()
	if d == nil {
		fmt.Println("daemon not running")
		return nil
	}
	// Ask the daemon to stop itself via control API if reachable, else signal.
	if cc, err := daemon.NewControlClient(); err == nil {
		cc.Post("/_control/shutdown", nil) // best effort
	}
	time.Sleep(300 * time.Millisecond)
	if daemon.ReadDaemonFile() != nil {
		if p, err := os.FindProcess(d.PID); err == nil {
			p.Kill()
		}
		os.Remove(filepath.Join(paths.Home(), "daemon.json"))
	}
	fmt.Println("daemon stopped")
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	rps := fs.Float64("rate", 20, "requests/sec per client")
	burst := fs.Int("burst", 60, "burst size")
	var keys stringList
	fs.Var(&keys, "api-key", "required API key (repeatable)")
	fs.Parse(args)

	st, err := store.Open()
	if err != nil {
		return err
	}
	loadState := func() (*store.State, int64, error) {
		s, err := st.Load()
		if err != nil {
			return nil, 0, err
		}
		var mod int64 = 1
		if fi, err := os.Stat(paths.StateFile()); err == nil {
			mod = fi.ModTime().UnixNano()
		}
		return s, mod, nil
	}
	router := proxy.NewRouter(loadState)
	srv, err := proxy.NewServer(proxy.Config{
		Addr: *addr, APIKeys: keys, RateRPS: *rps, RateBurst: *burst,
		Logger: slogStd(),
	}, router)
	if err != nil {
		return err
	}
	return srv.ListenAndServe(context.Background())
}

// ---- instances ----

func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	instance := fs.String("instance", "", "instance name (default: model id)")
	port := fs.Int("port", 0, "fixed port (default: auto)")
	var vars stringList
	fs.Var(&vars, "var", "backend var k=v (repeatable)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: llmctl start <model-id>")
	}
	modelID := fs.Arg(0)
	varMap := map[string]string{}
	for _, v := range vars {
		k, val, ok := strings.Cut(v, "=")
		if !ok {
			return fmt.Errorf("bad --var %q (want k=v)", v)
		}
		varMap[k] = val
	}

	// Prefer the daemon (it owns the child processes). Fall back to
	// supervising in-process if the daemon is down.
	if cc, err := daemon.NewControlClient(); err == nil {
		code, body, err := cc.Post("/_control/start", map[string]any{
			"instance": *instance, "model": modelID, "port": *port, "vars": varMap,
		})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s", strings.TrimSpace(string(body)))
		}
		var inst store.InstanceRecord
		json.Unmarshal(body, &inst)
		fmt.Printf("instance %q running on http://127.0.0.1:%d (model %s)\n", inst.ID, inst.Port, inst.ModelID)
		fmt.Printf("try: curl http://127.0.0.1:8080/v1/chat/completions -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'\n", modelID)
		return nil
	}
	fmt.Fprintln(os.Stderr, "daemon not running — starting instance in this process (stays up until you Ctrl-C or use 'llmctl up')")
	return startForeground(*instance, modelID, *port, varMap)
}

func startForeground(instance, modelID string, port int, vars map[string]string) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	reg, err := manifest.Builtin()
	if err != nil {
		return err
	}
	sv := newSupervisor(st, reg)
	if instance == "" {
		instance = modelID
	}
	inst, err := sv.Start(context.Background(), instance, modelID, port, vars)
	if err != nil {
		return err
	}
	fmt.Printf("instance %q running on port %d — press Ctrl-C to stop\n", inst.ID, inst.Port)
	select {} // block; SIGINT kills us and the reaper marks it stopped
}

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	force := fs.Bool("force", false, "skip graceful shutdown")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: llmctl stop <instance>")
	}
	id := fs.Arg(0)
	if cc, err := daemon.NewControlClient(); err == nil {
		code, body, err := cc.Post("/_control/stop", map[string]any{"instance": id, "force": *force})
		if err != nil {
			return err
		}
		if code != 200 {
			return fmt.Errorf("%s", strings.TrimSpace(string(body)))
		}
		fmt.Printf("instance %q stopped\n", id)
		return nil
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	reg, _ := manifest.Builtin()
	sv := newSupervisor(st, reg)
	if err := sv.Stop(id, *force); err != nil {
		return err
	}
	fmt.Printf("instance %q stopped\n", id)
	return nil
}

func cmdPS(args []string) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	if len(state.Instances) == 0 {
		fmt.Println("no instances (start one: llmctl start <model-id>)")
		return nil
	}
	tw := newTabWriter()
	fmt.Fprintln(tw, "INSTANCE\tMODEL\tBACKEND\tPORT\tSTATUS\tPID\tSTARTED")
	for _, id := range state.SortedInstanceIDs() {
		i := state.Instances[id]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%d\t%s\n",
			i.ID, i.ModelID, i.BackendID, i.Port, i.Status, i.PID, i.StartedAt.Local().Format("2006-01-02 15:04"))
	}
	tw.Flush()
	return nil
}

func cmdStatus(args []string) error {
	d := daemon.ReadDaemonFile()
	if d == nil {
		fmt.Println("daemon: stopped")
	} else {
		fmt.Printf("daemon: running (pid %d, gateway %s, control %s, since %s)\n",
			d.PID, d.GatewayAddr, d.ControlAddr, d.StartedAt.Local().Format(time.RFC3339))
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	fmt.Printf("backends: %d installed, models: %d installed, instances: %d\n",
		len(state.Backends), len(state.Models), len(state.Instances))
	return cmdPS(args)
}

// ---- backends ----

func cmdBackends(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl backends list|install|remove [id]")
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	reg, err := manifest.Builtin()
	if err != nil {
		return err
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		tw := newTabWriter()
		fmt.Fprintln(tw, "ID\tINSTALLED\tVERSION\tDESCRIPTION")
		for _, b := range reg.Backends {
			ver, inst := "-", "no"
			if rec, ok := state.Backends[b.ID]; ok {
				ver, inst = rec.Version, "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", b.ID, inst, ver, b.Description)
		}
		tw.Flush()
	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		asset := fs.String("asset", "", "asset hint, e.g. vulkan|cpu|cuda-cu12.4")
		// Go's flag pkg stops at the first positional arg; hoist flags first.
		fs.Parse(reorderFlags(args[1:]))
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: llmctl backends install <backend-id> [--asset hint]")
		}
		in := installer.New(st, reg)
		fmt.Printf("installing backend %q...\n", fs.Arg(0))
		if err := in.Install(context.Background(), fs.Arg(0), *asset, func(s string) { fmt.Println(" ", s) }); err != nil {
			return err
		}
		fmt.Println("done")
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl backends remove <backend-id>")
		}
		if err := installer.New(st, reg).Uninstall(args[1]); err != nil {
			return err
		}
		fmt.Printf("backend %q removed\n", args[1])
	default:
		return fmt.Errorf("unknown backends subcommand %q", args[0])
	}
	return nil
}

// ---- models ----

func cmdModels(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: llmctl models list|available|install|remove [id]")
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	reg, err := manifest.Builtin()
	if err != nil {
		return err
	}
	state, err := st.Load()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(state.Models) == 0 {
			fmt.Println("no models installed (see: llmctl models available)")
			return nil
		}
		tw := newTabWriter()
		fmt.Fprintln(tw, "ID\tBACKEND\tSIZE\tPATH")
		for id, m := range state.Models {
			fmt.Fprintf(tw, "%s\t%s\t%.1f GB\t%s\n", id, m.BackendID, float64(m.SizeBytes)/(1<<30), m.Path)
		}
		tw.Flush()
	case "available":
		tw := newTabWriter()
		fmt.Fprintln(tw, "ID\tBACKEND\tSIZE GB\tLICENSE\tSOURCE")
		for _, m := range reg.Models {
			lic := m.Source.License
			src := m.Source.Repo
			if m.Source.Kind == "url" {
				src = "(url)"
			}
			fmt.Fprintf(tw, "%s\t%s\t%.1f\t%s\t%s\n", m.ID, m.Backend, m.SizeGB, lic, src)
		}
		tw.Flush()
		fmt.Println("\nany HF GGUF repo also works: llmctl models install --hf <repo> [--file <name.gguf>]")
	case "install":
		return modelsInstall(st, reg, args[1:])
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: llmctl models remove <model-id>")
		}
		id := args[1]
		if rec, ok := state.Models[id]; ok {
			os.RemoveAll(filepath.Dir(rec.Path))
		}
		return st.Update(func(s *store.State) error { delete(s.Models, id); return nil })
	default:
		return fmt.Errorf("unknown models subcommand %q", args[0])
	}
	return nil
}

func modelsInstall(st *store.Store, reg *manifest.Registry, args []string) error {
	fs := flag.NewFlagSet("models install", flag.ExitOnError)
	hfRepo := fs.String("hf", "", "install any GGUF from a HuggingFace repo")
	hfFile := fs.String("file", "", "specific .gguf file within the --hf repo")
	fs.Parse(args)

	if *hfRepo != "" {
		return installFromHF(st, *hfRepo, *hfFile)
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: llmctl models install <model-id> | --hf <repo> [--file f.gguf]")
	}
	mdef, ok := reg.Model(fs.Arg(0))
	if !ok {
		return fmt.Errorf("unknown model %q (see: llmctl models available)", fs.Arg(0))
	}
	fmt.Printf("downloading %s (%.1f GB)...\n", mdef.Name, mdef.SizeGB)
	dest, err := downloader.Download(context.Background(), mdef, progressPrinter())
	if err != nil {
		return fmt.Errorf("download failed (re-run to resume): %w", err)
	}
	sum, _ := downloader.Checksum(dest)
	fi, _ := os.Stat(dest)
	return st.Update(func(s *store.State) error {
		s.Models[mdef.ID] = &store.ModelRecord{
			ID: mdef.ID, BackendID: mdef.Backend, Path: dest,
			SizeBytes: fi.Size(), DownloadedAt: time.Now().UTC(),
		}
		fmt.Printf("installed %s -> %s (sha256 %s...)\n", mdef.ID, dest, sum[:16])
		return nil
	})
}

func installFromHF(st *store.Store, repo, file string) error {
	fmt.Printf("querying HuggingFace %s...\n", repo)
	entries, err := downloader.HFCatalog(repo, "")
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no .gguf files found in %s", repo)
	}
	chosen := entries[0]
	if file != "" {
		found := false
		for _, e := range entries {
			if strings.EqualFold(filepath.Base(e.Path), file) || strings.EqualFold(e.Path, file) {
				chosen, found = e, true
			}
		}
		if !found {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Path)
			}
			return fmt.Errorf("file %q not in repo; available: %s", file, strings.Join(names, ", "))
		}
	} else if len(entries) > 1 {
		fmt.Println("multiple GGUFs found, picking smallest:")
		for _, e := range entries {
			fmt.Printf("  %s (%.1f GB)\n", e.Path, float64(e.SizeBytes)/(1<<30))
		}
		min := entries[0]
		for _, e := range entries[1:] {
			if e.SizeBytes < min.SizeBytes {
				min = e
			}
		}
		chosen = min
		fmt.Printf("chosen: %s\n", chosen.Path)
	}
	id := sanitizeID(repo) + "-" + sanitizeID(filepath.Base(chosen.Path))
	mdef := &manifest.ModelDef{
		ID:      id,
		Name:    repo + " / " + chosen.Path,
		Backend: "llama.cpp",
		Format:  "gguf",
		Source: manifest.ModelSource{
			Kind: "huggingface", Repo: repo, Filename: chosen.Path,
		},
	}
	fmt.Printf("downloading %s (%.1f GB)...\n", chosen.Path, float64(chosen.SizeBytes)/(1<<30))
	dest, err := downloader.Download(context.Background(), mdef, progressPrinter())
	if err != nil {
		return fmt.Errorf("download failed (re-run to resume): %w", err)
	}
	fi, _ := os.Stat(dest)
	return st.Update(func(s *store.State) error {
		s.Models[id] = &store.ModelRecord{
			ID: id, BackendID: "llama.cpp", Path: dest,
			SizeBytes: fi.Size(), DownloadedAt: time.Now().UTC(),
		}
		fmt.Printf("installed %s -> %s\n", id, dest)
		return nil
	})
}

// ---- misc ----

func cmdKey(args []string) error {
	fmt.Println(proxy.GenerateKey())
	fmt.Fprintln(os.Stderr, "\nuse with: llmctl up --api-key <key> ; clients send Authorization: Bearer <key>")
	return nil
}

// ---- helpers ----

func progressPrinter() downloader.ProgressFn {
	start := time.Now()
	return func(p downloader.Progress) {
		if p.Done {
			fmt.Printf("\rdownloaded %.2f GB in %s\n", float64(p.Bytes)/(1<<30), time.Since(start).Round(time.Second))
			return
		}
		if p.Total > 0 {
			pct := 100 * float64(p.Bytes) / float64(p.Total)
			fmt.Printf("\r%.1f%% (%.2f/%.2f GB, %.1f MB/s)  ",
				pct, float64(p.Bytes)/(1<<30), float64(p.Total)/(1<<30), p.SpeedBPS/(1<<20))
		} else {
			fmt.Printf("\r%.2f GB (%.1f MB/s)  ", float64(p.Bytes)/(1<<30), p.SpeedBPS/(1<<20))
		}
	}
}

func sanitizeID(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
	return strings.Trim(s, "-")
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func newSupervisor(st *store.Store, reg *manifest.Registry) *supervisor.Supervisor {
	return supervisor.New(st, reg, slogStd())
}

func slogStd() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
}

// reorderFlags moves flag args (leading '-' and their values) ahead of
// positional args, so Go's flag package parses them even when the user
// typed `sub id --flag value`.
func reorderFlags(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" { // everything after is positional
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			// "--flag value" (space-separated): hoist the value too.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}
