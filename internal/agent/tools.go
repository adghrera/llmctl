package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"llmctl/internal/websearch"
)

// WebSearchTool adapts websearch.Searcher to the Tool interface.
type WebSearchTool struct {
	S      *websearch.Searcher
	MaxRes int // per-query result cap (default 5)
}

func (t *WebSearchTool) Name() string { return "web_search" }

type searchArgs struct {
	Query string `json:"query"`
}

func (t *WebSearchTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	var a searchArgs
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil || strings.TrimSpace(a.Query) == "" {
		return "", nil, fmt.Errorf(`invalid args: expected {"query": "..."}`)
	}
	max := t.MaxRes
	if max <= 0 {
		max = 5
	}
	results, err := t.S.Search(ctx, a.Query, max)
	if err != nil {
		return "", nil, err
	}
	// Compact text form for the model (keeps context small).
	var sb strings.Builder
	if len(results) == 0 {
		sb.WriteString("(no results)")
	}
	for i, r := range results {
		snippet := r.Snippet
		if len(snippet) > 400 {
			snippet = snippet[:400] + "…"
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, snippet))
	}
	return sb.String(), results, nil
}
