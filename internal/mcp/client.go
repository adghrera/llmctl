package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// protocolVersion is the MCP protocol revision we speak.
const protocolVersion = "2024-11-05"

// rpcRequest is a JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Client is a connected MCP server session (stdio transport).
type Client struct {
	srv   Server
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	next  int
	wait  map[int]chan rpcResponse
	tools []ToolDef
}

// Connect spawns the server process and performs the initialize + tools/list
// handshake. The caller owns the Client and must Close it.
func Connect(ctx context.Context, srv Server) (*Client, error) {
	// The process must outlive the request that spawned it: the registry
	// caches clients across requests, so bind the process to a detached
	// context (Close/CloseAll terminates it) while the handshake below
	// still honors the caller's deadline (F834).
	cmd := exec.CommandContext(context.WithoutCancel(ctx), srv.Command, srv.Args...)
	cmd.Env = os.Environ()
	for k, v := range srv.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", srv.Command, err)
	}
	c := &Client{srv: srv, cmd: cmd, stdin: stdin, wait: map[int]chan rpcResponse{}}
	go c.readLoop(bufio.NewScanner(stdout))
	// Handshake with a bounded timeout.
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := c.rpc(ctx2, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "llmctl", "version": "0.2"},
	}); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.notify("notifications/initialized", map[string]any{})
	res, err := c.rpc(ctx2, "tools/list", map[string]any{})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp: tools/list: %w", err)
	}
	var tr struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(res, &tr); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp: bad tools/list: %w", err)
	}
	c.tools = tr.Tools
	return c, nil
}

// Tools returns the advertised tools.
func (c *Client) Tools() []ToolDef { return c.tools }

// Server returns the config this client connected to.
func (c *Client) Server() Server { return c.srv }

// CallTool invokes a tool and flattens text content into a string.
func (c *Client) CallTool(ctx context.Context, name string, argsJSON string) (string, error) {
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("mcp: invalid arguments JSON: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var out struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("mcp: bad tools/call result: %w", err)
	}
	var sb []byte
	for _, part := range out.Content {
		if part.Type == "text" {
			sb = append(sb, part.Text...)
			sb = append(sb, '\n')
		}
	}
	text := string(sb)
	if out.IsError {
		return text, fmt.Errorf("mcp: tool %q returned an error: %s", name, text)
	}
	return text, nil
}

// Close terminates the server process.
func (c *Client) Close() {
	c.stdin.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	c.mu.Lock()
	for _, ch := range c.wait {
		close(ch)
	}
	c.wait = map[int]chan rpcResponse{}
	c.mu.Unlock()
}

// rpc sends a request and waits for its response.
func (c *Client) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan rpcResponse, 1)
	c.wait[id] = ch
	c.mu.Unlock()

	err := c.send(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		c.mu.Lock()
		delete(c.wait, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case resp, ok := <-ch:
		c.mu.Lock()
		delete(c.wait, id)
		c.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("mcp: connection closed waiting for %s", method)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp: %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.wait, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp: timeout waiting for %s", method)
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

// readLoop dispatches responses to waiting rpc calls; notifications and
// unknown ids are ignored.
func (c *Client) readLoop(sc *bufio.Scanner) {
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if json.Unmarshal(line, &resp) != nil || resp.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch, ok := c.wait[resp.ID]
		c.mu.Unlock()
		if ok {
			select {
			case ch <- resp:
			default:
			}
		}
	}
	// EOF: wake everyone waiting.
	c.mu.Lock()
	for id, ch := range c.wait {
		close(ch)
		delete(c.wait, id)
	}
	c.mu.Unlock()
}
