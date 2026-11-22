// Package webhook posts JSON payloads to user-configured URLs (e.g. a
// scheduled task notifying a chat platform when it finishes). Best-effort:
// callers log failures; delivery is at-most-once with a short timeout.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Post sends v as JSON to url with Content-Type application/json. Returns an
// error if the URL is not http(s) or the request fails / returns >= 300.
func Post(ctx context.Context, url string, v any) error {
	return PostWithSecret(ctx, url, v, "")
}

// PostWithSecret is Post plus an HMAC-SHA256 signature of the raw JSON body
// sent as "X-LLMCtrl-Signature: sha256=<hex>" when secret is non-empty, so
// receivers can verify the payload really came from this daemon (F111).
func PostWithSecret(ctx context.Context, url string, v any, secret string) error {
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
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(b)
		req.Header.Set("X-LLMCtrl-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
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
