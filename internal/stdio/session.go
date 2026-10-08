// Package stdio implements the llmctl-stdio/1 backend protocol: JSON-lines
// over a child process's stdin/stdout. It lets llmctl talk to a backend over
// IPC (no HTTP port, no network overhead) and multiplex many concurrent
// requests over a single pipe by correlating responses on request id.
//
// Protocol (one JSON object per line):
//
//	server -> client (once, on startup):
//	  {"protocol":"llmctl-stdio/1","name":...,"models":[...]}
//
//	client -> server (a request):
//	  {"id":"<uuid>","method":"chat.completions","params":{...openai body...}}
//
//	server -> client (0..n chunks, then a terminal message):
//	  {"id":"<uuid>","type":"chunk","data":{...openai chunk...}}
//	  {"id":"<uuid>","type":"done","data":{...final...}}
//	  {"id":"<uuid>","type":"error","error":{"message":...}}
//
//	keepalive: {"id":"<uuid>","method":"ping"} -> {"id":"<uuid>","type":"pong"}
package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Protocol is the wire version this client speaks.
const Protocol = "llmctl-stdio/1"

// Msg is one decoded server->client line.
type Msg struct {
	ID    string          `json:"id"`
	Type  string          `json:"type,omitempty"` // chunk | done | error | pong
	Data  json.RawMessage `json:"data,omitempty"`
	Error *WireError      `json:"error,omitempty"`
	// Hello-only fields.
	Protocol string   `json:"protocol,omitempty"`
	Name     string   `json:"name,omitempty"`
	Models   []string `json:"models,omitempty"`
}

// WireError is the error payload in an error message.
type WireError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
}

// ErrSessionClosed is returned when the backend process has exited.
var ErrSessionClosed = errors.New("stdio backend exited")

// Session is a live connection to one stdio backend process.
type Session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu      sync.Mutex
	pending map[string]chan *Msg
	closed  bool
	closeCh chan struct{}
	closeOnce sync.Once

	readyCh chan struct{} // closed when the hello line is read
	hello   *Msg
	helloErr error
}

// NewSession attaches to an already-started command that has stdin/stdout
// pipes. The caller must have called cmd.Start() with StdinPipe/StdoutPipe.
func NewSession(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser) *Session {
	s := &Session{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending: map[string]chan *Msg{},
		closeCh: make(chan struct{}),
		readyCh: make(chan struct{}),
	}
	go s.readLoop()
	return s
}

// readLoop reads stdout line-by-line and dispatches each message to the
// waiter registered for its id (or resolves the hello if it's the first).
func (s *Session) readLoop() {
	sc := bufio.NewScanner(s.stdout)
	// Generous buffer: model paths and long tokens can exceed 64KB.
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m Msg
		if err := json.Unmarshal(line, &m); err != nil {
			// Non-JSON (e.g. a backend log line that leaked to stdout).
			// Ignore; real protocol lines are JSON.
			continue
		}
		s.mu.Lock()
		if m.ID == "" {
			// Hello / broadcast line.
			if s.hello == nil {
				h := m
				s.hello = &h
				s.mu.Unlock()
				close(s.readyCh)
			}
			s.mu.Unlock()
			continue
		}
		ch, ok := s.pending[m.ID]
		if !ok {
			s.mu.Unlock()
			continue
		}
		// Terminal messages remove the waiter; chunks keep it.
		if m.Type == "done" || m.Type == "error" || m.Type == "pong" {
			delete(s.pending, m.ID)
		}
		s.mu.Unlock()
		select {
		case ch <- &m:
		default:
			// Waiter gone; drop.
		}
	}
	// stdout EOF: backend exited.
	s.mu.Lock()
	s.closed = true
	for id, ch := range s.pending {
		delete(s.pending, id)
		select {
		case ch <- &Msg{ID: id, Type: "error", Error: &WireError{Message: ErrSessionClosed.Error()}}:
		default:
		}
	}
	s.mu.Unlock()
	close(s.readyCh) // unblock any hello waiters
	close(s.closeCh)
}

// Hello waits for the startup hello line (with a timeout) and returns it.
func (s *Session) Hello(timeout time.Duration) (*Msg, error) {
	select {
	case <-s.readyCh:
		if s.helloErr != nil {
			return nil, s.helloErr
		}
		return s.hello, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out waiting for %s hello", Protocol)
	}
}

// Call sends a request and returns a channel that yields its messages
// (chunks, then a terminal done/error). The channel is closed after the
// terminal message.
func (s *Session) Call(ctx context.Context, id, method string, params any) (<-chan *Msg, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSessionClosed
	}
	ch := make(chan *Msg, 64)
	s.pending[id] = ch
	s.mu.Unlock()

	body := map[string]any{"id": id, "method": method, "params": params}
	b, err := json.Marshal(body)
	if err != nil {
		s.drop(id)
		return nil, err
	}
	b = append(b, '\n')
	if _, err := s.stdin.Write(b); err != nil {
		s.drop(id)
		return nil, fmt.Errorf("write to backend: %w", err)
	}

	out := make(chan *Msg)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				s.drop(id)
				return
			case m, ok := <-ch:
				if !ok {
					return
				}
				out <- m
				if m.Type == "done" || m.Type == "error" {
					return
				}
			}
		}
	}()
	return out, nil
}

func (s *Session) drop(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

// Close terminates the backend process and closes the pipes.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		_ = s.stdin.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
}

// Process returns the underlying OS process (for PID / liveness).
func (s *Session) Process() *os.Process {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process
}

// Closed is closed when the backend process exits (stdout EOF).
func (s *Session) Closed() <-chan struct{} { return s.closeCh }
