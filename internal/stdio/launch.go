package stdio

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// LaunchConfig describes how to spawn a stdio backend.
type LaunchConfig struct {
	Binary string
	Args   []string
	Env    map[string]string
	Dir    string // working dir (default: dir of binary)
	// StderrLog, if set, receives the backend's stderr. stdout is the
	// protocol channel and is never logged.
	StderrLog string
	// ReadyTimeout is how long to wait for the hello line.
	ReadyTimeout time.Duration
}

// Launch spawns the backend, waits for its hello, and returns a live Session.
// The caller owns the returned Session and must Close() it. The backend is a
// direct child of llmctl; llmctl reaps it via the Session's cmd.Wait().
func Launch(cfg LaunchConfig) (*Session, *Msg, error) {
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	cmd := exec.Command(cfg.Binary, cfg.Args...)
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	} else if cfg.Binary != "" {
		cmd.Dir = filepath.Dir(cfg.Binary)
	}
	if len(cfg.Env) > 0 {
		env := os.Environ()
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if cfg.StderrLog != "" {
		f, err := os.OpenFile(cfg.StderrLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("stderr log: %w", err)
		}
		cmd.Stderr = f
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("spawn %s: %w", cfg.Binary, err)
	}
	s := NewSession(cmd, stdin, stdout)
	hello, err := s.Hello(cfg.ReadyTimeout)
	if err != nil {
		s.Close()
		return nil, nil, err
	}
	return s, hello, nil
}
