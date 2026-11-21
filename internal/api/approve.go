package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"llmctl/internal/agent"
)

// approvalTimeout bounds how long the agent loop waits for a user decision.
const approvalTimeout = 2 * time.Minute

// pendingApprovals tracks tool calls waiting for a user decision, keyed by a
// random token sent to the client in the approval event (F055).
type pendingApprovals struct {
	mu sync.Mutex
	m  map[string]chan bool
}

func newPendingApprovals() *pendingApprovals {
	return &pendingApprovals{m: map[string]chan bool{}}
}

func (p *pendingApprovals) add(id string, ch chan bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[id] = ch
}

func (p *pendingApprovals) resolve(id string, approve bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.m[id]
	if !ok {
		return false
	}
	delete(p.m, id)
	ch <- approve
	return true
}

func (p *pendingApprovals) remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, id)
}

func approvalToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// approveToolCall handles POST /api/v1/agent/approve {"id": "...", "approve": true}.
func (s *Server) approveToolCall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if !s.approvals.resolve(body.ID, body.Approve) {
		writeErr(w, 404, "no pending approval with that id (already resolved or expired)")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// makeApprover returns the agent.Request.Approve callback for one SSE stream.
// It emits an approval event and blocks until the user answers or times out.
func (s *Server) makeApprover(send func(agent.Event)) func(context.Context, string, string) bool {
	return func(ctx context.Context, name, argsJSON string) bool {
		id := approvalToken()
		ch := make(chan bool, 1)
		s.approvals.add(id, ch)
		defer s.approvals.remove(id)
		send(agent.Event{Type: "approval", ID: id, Name: name, Query: argsJSON})
		timer := time.NewTimer(approvalTimeout)
		defer timer.Stop()
		select {
		case ok := <-ch:
			return ok
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		}
	}
}
