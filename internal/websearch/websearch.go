// Package websearch provides keyless web search by scraping free endpoints:
// DuckDuckGo's HTML endpoint (primary) with Bing's RSS endpoint as fallback.
// No API keys, no accounts — results are title/url/snippet triples.
package websearch

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Result is one search hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Searcher performs searches. Zero value is not usable; use New.
type Searcher struct {
	client *http.Client
	ua     string
}

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

func New() *Searcher {
	return &Searcher{
		client: &http.Client{Timeout: 15 * time.Second},
		ua:     browserUA,
	}
}

// Search runs the query: DuckDuckGo first, Bing RSS fallback. Returns up to
// maxResults hits (clamped 1..10).
func (s *Searcher) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}
	if maxResults < 1 {
		maxResults = 5
	}
	if maxResults > 10 {
		maxResults = 10
	}

	results, ddgErr := s.duckduckgo(ctx, query, maxResults)
	if len(results) > 0 {
		return results, nil
	}
	bingResults, bingErr := s.bingRSS(ctx, query, maxResults)
	if len(bingResults) > 0 {
		return bingResults, nil
	}
	if ddgErr != nil && bingErr != nil {
		return nil, fmt.Errorf("duckduckgo: %v; bing: %v", ddgErr, bingErr)
	}
	return nil, nil // both reachable, zero hits
}

func (s *Searcher) get(ctx context.Context, rawurl string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MiB cap
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// ---- DuckDuckGo (HTML endpoint) ----

var (
	// <a rel="nofollow" class="result__a" href="...">Title</a>
	ddgLinkRe  = regexp.MustCompile(`(?s)class="result__a"\s+href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgSnipRe  = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>|class="result__snippet"[^>]*>(.*?)</td>`)
	ddgAnySnip = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</(?:a|td|div|span)>`)
	tagRe      = regexp.MustCompile(`<[^>]*>`)
)

func (s *Searcher) duckduckgo(ctx context.Context, query string, max int) ([]Result, error) {
	body, err := s.get(ctx, "https://html.duckduckgo.com/html/?q="+url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	return parseDDGHTML(body, max)
}

func parseDDGHTML(body []byte, max int) ([]Result, error) {
	html := string(body)
	links := ddgLinkRe.FindAllStringSubmatch(html, -1)
	if len(links) == 0 {
		return nil, fmt.Errorf("no results parsed")
	}
	// Snippets, positionally.
	snips := ddgAnySnip.FindAllStringSubmatch(html, -1)
	out := make([]Result, 0, max)
	for i, m := range links {
		if len(out) >= max {
			break
		}
		rawURL := htmlUnescape(strings.TrimSpace(m[1]))
		// Strip real HTML tags BEFORE unescaping entities, so escaped
		// text like &lt;code&gt; survives as literal characters.
		title := strings.TrimSpace(htmlUnescape(tagRe.ReplaceAllString(m[2], "")))
		// DDG wraps URLs: //duckduckgo.com/l/?uddg=<enc>&rut=...
		rawURL = unwrapDDGRedirect(rawURL)
		if rawURL == "" || title == "" {
			continue
		}
		snippet := ""
		if i < len(snips) {
			snippet = strings.TrimSpace(htmlUnescape(tagRe.ReplaceAllString(snips[i][1], "")))
		}
		out = append(out, Result{Title: title, URL: rawURL, Snippet: snippet})
	}
	return out, nil
}

func unwrapDDGRedirect(href string) string {
	if u, err := url.Parse(href); err == nil {
		if q := u.Query().Get("uddg"); q != "" {
			return q
		}
		if strings.HasPrefix(href, "//") {
			return ""
		}
	}
	return href
}

// ---- Bing (RSS endpoint — clean XML, no scraping) ----

type bingRSSDoc struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (s *Searcher) bingRSS(ctx context.Context, query string, max int) ([]Result, error) {
	body, err := s.get(ctx, "https://www.bing.com/search?q="+url.QueryEscape(query)+"&format=rss")
	if err != nil {
		return nil, err
	}
	return parseBingRSS(body, max)
}

func parseBingRSS(body []byte, max int) ([]Result, error) {
	var doc bingRSSDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make([]Result, 0, max)
	for _, it := range doc.Channel.Items {
		if len(out) >= max {
			break
		}
		title := strings.TrimSpace(htmlUnescape(it.Title))
		link := strings.TrimSpace(it.Link)
		snippet := strings.TrimSpace(htmlUnescape(tagRe.ReplaceAllString(it.Description, "")))
		if title == "" || link == "" {
			continue
		}
		out = append(out, Result{Title: title, URL: link, Snippet: snippet})
	}
	return out, nil
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}
