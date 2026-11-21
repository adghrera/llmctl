// Package webhook posts JSON payloads to user-configured URLs (e.g. a
// scheduled task notifying a chat platform when it finishes). Best-effort:
// callers log failures; delivery is at-most-once with a short timeout.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Post sends v as JSON to url with Content-Type application/json. Returns an
// error if the URL is not http(s) or the request fails / returns >= 300.
func Post(ctx context.Context, url string, v any) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("webhook: url must be http(s), got %q", url)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("webhook: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "llmctl-webhook")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: %s returned %d", url, resp.StatusCode)
	}
	return nil
}
