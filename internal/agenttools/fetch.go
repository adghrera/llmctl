package agenttools

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- fetch_url -------------------------------------------------------------

const (
	fetchMaxBytes   = 512 << 10 // read cap
	fetchMaxText    = 12000     // returned text cap
	fetchTimeout    = 20 * time.Second
	fetchMaxRecords = 3 // redirects followed
)

type FetchURLTool struct {
	Client *http.Client // nil = default with redirect guard
	// AllowPrivate disables the SSRF guard. Test-only; never set by the API.
	AllowPrivate bool
}

func (t *FetchURLTool) Name() string { return "fetch_url" }

func (t *FetchURLTool) client() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	allowPrivate := t.AllowPrivate
	return &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > fetchMaxRecords {
				return http.ErrUseLastResponse
			}
			if allowPrivate {
				return nil
			}
			// re-run the SSRF guard on every redirect target
			if err := guardURL(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
}

// guardURL rejects non-http(s) schemes and private/loopback/link-local
// targets (SSRF protection: the daemon may run next to sensitive ports).
func guardURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (http/https only)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		return guardIP(ip)
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".local") || strings.HasSuffix(strings.ToLower(host), ".internal") {
		return fmt.Errorf("host %q not allowed", host)
	}
	// resolve DNS and guard every address
	addrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, a := range addrs {
		if err := guardIP(a.IP); err != nil {
			return fmt.Errorf("host %s resolves to a blocked address", host)
		}
	}
	return nil
}

func guardIP(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("address %s not allowed", ip)
	}
	// CGNAT 100.64.0.0/10 (Tailscale etc.)
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return fmt.Errorf("address %s not allowed", ip)
	}
	return nil
}

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style|noscript|svg)[^>]*>.*?</(script|style|noscript|svg)>`)
	tagRe         = regexp.MustCompile(`(?s)<[^>]*>`)
	wsRe          = regexp.MustCompile(`[ 	]+`)
	nlRe          = regexp.MustCompile(`\n{3,}`)
)

func (t *FetchURLTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	target, err := parseArg(argsJSON, "url", "uri")
	if err != nil {
		return "", nil, err
	}
	if target == "" {
		return "", nil, fmt.Errorf("fetch_url: url argument required")
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", nil, fmt.Errorf("fetch_url: bad url: %w", err)
	}
	if !t.AllowPrivate {
		if err := guardURL(u); err != nil {
			return "", nil, fmt.Errorf("fetch_url: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; llmctl/0.2; +local)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
	resp, err := t.client().Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("fetch_url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("fetch_url: HTTP %d from %s", resp.StatusCode, u.Host)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "image/") || strings.Contains(ct, "application/pdf") || strings.Contains(ct, "zip") {
		return "", nil, fmt.Errorf("fetch_url: %s is a %s file, not readable text", u.Host, ct)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("fetch_url: read: %w", err)
	}
	text := string(raw)
	if strings.Contains(ct, "html") || looksLikeHTML(text) {
		text = htmlToText(text)
	}
	truncated := len(raw) > fetchMaxBytes
	if len(text) > fetchMaxText {
		text = text[:fetchMaxText]
		truncated = true
	}
	out := fmt.Sprintf("Fetched %s (HTTP %d%s)\n\n%s", u.String(), resp.StatusCode,
		map[bool]string{true: ", truncated", false: ""}[truncated], text)
	results := []map[string]any{{"url": u.String(), "title": resp.Request.URL.Host, "status": resp.StatusCode}}
	return out, results, nil
}

func looksLikeHTML(s string) bool {
	if len(s) > 512 {
		s = s[:512]
	}
	lt := strings.Count(s, "<")
	return lt > 5
}

var entityRe2 = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ", "&mdash;", "—", "&ndash;", "–", "&rsquo;", "'", "&lsquo;", "'", "&ldquo;", `"`, "&rdquo;", `"`, "&hellip;", "…", "&copy;", "©", "&reg;", "®", "&trade;", "™", "&eacute;", "é", "&egrave;", "è", "&agrave;", "à", "&ccedil;", "ç", "&ucirc;", "û")

// htmlToText is a deliberately crude HTML→text pass: drop scripts/styles,
// turn block ends into newlines, strip tags, unescape common entities.
func htmlToText(s string) string {
	s = scriptStyleRe.ReplaceAllString(s, " ")
	// keep link hrefs visible: <a href="x">t</a> -> t (x)
	linkRe := regexp.MustCompile(`(?is)<a[^>]*href="([^"]{1,300})"[^>]*>(.*?)</a>`)
	s = linkRe.ReplaceAllString(s, "$2 ($1)")
	// block-level closers become newlines
	blockRe := regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|table|section|article|header|footer|blockquote|pre|ul|ol|br|hr)>`)
	s = blockRe.ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, " ")
	s = entityRe2.Replace(s)
	s = wsRe.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	s = strings.Join(out, "\n")
	s = nlRe.ReplaceAllString(s, "\n\n")
	return s
}

// --- calculator --------------------------------------------------------------

// --- helpers -----------------------------------------------------------------

// parseArg pulls the first present string key from a JSON args object
// without full struct plumbing (args are small).
func parseArg(argsJSON string, keys ...string) (string, error) {
	m := argRe.FindAllStringSubmatch(argsJSON, -1)
	for _, k := range keys {
		for _, pair := range m {
			if strings.EqualFold(pair[1], k) {
				return unquoteJSON(pair[2])
			}
		}
	}
	return "", nil
}

var argRe = regexp.MustCompile(`"(?i)([a-z0-9_]+)"\s*:\s*("(?:\\.|[^"\\])*"|\d+(?:\.\d+)?|true|false|null)`)

func unquoteJSON(v string) (string, error) {
	if strings.HasPrefix(v, `"`) {
		b, err := strconv.Unquote(v)
		if err != nil {
			// tolerate invalid escapes: strip quotes and backslashes
			return strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`), nil
		}
		return b, nil
	}
	return v, nil
}

var _ = time.Now
