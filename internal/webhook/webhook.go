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
	"sync"
	"time"
)

// Delivery is one webhook POST attempt kept in a small in-memory ring so the
// UI can show what was sent and whether it landed (F144).
type Delivery struct {
	Time   time.Time `json:"time"`
	URL    string    `json:"url"`
	Event  string    `json:"event,omitempty"` // "event" field of the payload, if any
	Status int       `json:"status,omitempty"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
}

const deliveryLogSize = 50

var (
	histMu  sync.Mutex
	histBuf []Delivery
)

func record(d Delivery) {
	histMu.Lock()
	defer histMu.Unlock()
	histBuf = append(histBuf, d)
	if len(histBuf) > deliveryLogSize {
		histBuf = append(histBuf[:0], histBuf[len(histBuf)-deliveryLogSize:]...)
	}
}

// History returns up to n recent delivery attempts, newest first.
func History(n int) []Delivery {
	histMu.Lock()
	defer histMu.Unlock()
	out := make([]Delivery, 0, n)
	for i := len(histBuf) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, histBuf[i])
	}
	return out
}

// Post sends v as JSON to url with Content-Type application/json. Returns an
// error if the URL is not http(s) or the request fails / returns >= 300.
func Post(ctx context.Context, url string, v any) error {
	return PostWithSecret(ctx, url, v, "")
}

// PostWithRetries is PostWithSecret with up to attempts total tries, pausing
// 1s, 2s, … between them. Each attempt is recorded in the delivery ring.
// Returns the last error, or nil as soon as one attempt lands (F163).
func PostWithRetries(ctx context.Context, url string, v any, secret string, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * time.Second):
			}
		}
		if err = PostWithSecret(ctx, url, v, secret); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
	}
	return err
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
	err = wrapDo(err, resp, url)
	record(deliveryFrom(url, b, resp, err))
	return err
}

// wrapDo turns a transport error or a >= 300 status into an error.
func wrapDo(err error, resp *http.Response, url string) error {
	if err != nil {
		return fmt.Errorf("webhook: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: %s returned %d", url, resp.StatusCode)
	}
	return nil
}

// deliveryFrom builds a ring entry; the event label is the payload's "event"
// field when present (best-effort).
func deliveryFrom(url string, body []byte, resp *http.Response, err error) Delivery {
	d := Delivery{Time: time.Now().UTC(), URL: url, OK: err == nil, Error: ""}
	if err != nil {
		d.Error = err.Error()
	}
	if resp != nil {
		d.Status = resp.StatusCode
	}
	var probe struct {
		Event string `json:"event"`
	}
	if json.Unmarshal(body, &probe) == nil {
		d.Event = probe.Event
	}
	return d
}
