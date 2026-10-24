package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"llmctl/internal/chatstore"
)

// Chat conversations (F025): persisted JSON files, one per conversation.
// The Chat UI saves the whole conversation after each completed turn and
// on navigation; the server treats each PUT as the authoritative state.

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	list, err := s.chats.List()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	c, err := s.chats.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, c)
}

// saveChat creates (id empty) or replaces a conversation.
func (s *Server) saveChat(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	var c chatstore.Conversation
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&c); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	// sanitize roles; keep content as-is
	msgs := make([]chatstore.Message, 0, len(c.Messages))
	for _, m := range c.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" && len(m.Tools) == 0 {
			continue
		}
		msgs = append(msgs, m)
	}
	c.Messages = msgs
	// Compute context-window usage for this conversation. Estimate tokens
	// for messages that don't carry real usage values.
	used := chatstore.ConversationTokens(msgs)
	limit := 0
	if c.Model != "" && s.store != nil {
		if st, err := s.store.Load(); err == nil {
			if m, ok := st.Models[c.Model]; ok {
				limit = m.CtxSize
			}
		}
	}
	c.Context = chatstore.ComputeContext(used, limit)

	if id := r.PathValue("id"); id != "" {
		c.ID = id // path wins over body
	}
	if len(c.Messages) == 0 {
		writeErr(w, 400, "conversation has no messages")
		return
	}
	saved, err := s.chats.Save(&c)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "invalid chat id") {
			writeErr(w, 404, err.Error())
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, saved)
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	if err := s.chats.Delete(r.PathValue("id")); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, 404, err.Error())
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// exportChat handles GET /api/v1/chats/{id}/export?format=json|markdown.
func (s *Server) exportChat(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	id := r.PathValue("id")
	c, err := s.chats.Get(id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	format := r.URL.Query().Get("format")
	switch format {
	case "markdown", "md":
		md := exportChatMarkdown(c)
		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("Content-Disposition", `attachment; filename="`+c.Title+`.md"`)
		w.WriteHeader(200)
		w.Write([]byte(md))
		return
	case "json", "":
		b, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+c.ID+`.json"`)
		w.WriteHeader(200)
		w.Write(b)
		return
	default:
		writeErr(w, 400, "format must be json or markdown")
		return
	}
}

// exportChatMarkdown renders a conversation as Markdown.
func exportChatMarkdown(c *chatstore.Conversation) string {
	var b strings.Builder
	b.WriteString("# " + c.Title + "\n\n")
	b.WriteString("**Model:** " + c.Model + "\n")
	b.WriteString("**Agent mode:** " + fmt.Sprintf("%v", c.AgentMode) + "\n")
	b.WriteString("**Created:** " + c.CreatedAt.Format("2006-01-02 15:04:05") + "\n")
	b.WriteString("**Updated:** " + c.UpdatedAt.Format("2006-01-02 15:04:05") + "\n\n")
	b.WriteString("---\n\n")
	for _, m := range c.Messages {
		role := strings.ToUpper(string(m.Role[0])) + m.Role[1:]
		b.WriteString("## " + role + "\n\n")
		b.WriteString(m.Content + "\n\n")
		if len(m.Tools) > 0 {
			b.WriteString("**Tools used:**\n")
			for _, t := range m.Tools {
				b.WriteString("- " + t.Name + ": " + t.Query + "\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// forkChat handles POST /api/v1/chats/{id}/fork.
// Body: {"forkAt": <number of leading messages to keep>, "title": "<new title>"}
func (s *Server) forkChat(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	var req struct {
		ForkAt int    `json:"forkAt"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	c, err := s.chats.Fork(r.PathValue("id"), req.ForkAt, req.Title)
	if err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, 201, c)
}

// searchChats handles GET /api/v1/chats/search?q=.
// Returns conversations whose title or message content contains q.
func (s *Server) searchChats(w http.ResponseWriter, r *http.Request) {
	if s.chats == nil {
		writeErr(w, 503, "chat store unavailable")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, 400, "q parameter required")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	maxMatches := 5
	if v := r.URL.Query().Get("maxMatches"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxMatches = n
		}
	}
	results, err := s.chats.Search(q, limit, maxMatches)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, results)
}
