package websearch

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

const ddgFixture = `<html><body>
<div class="result results_links">
<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fblog%2Fgo1.27&amp;rut=abc">Go 1.27 is released</a>
<a class="result__snippet" href="#">The Go team is pleased to announce the release of Go 1.27.</a>
</div>
<div class="result">
<a rel="nofollow" class="result__a" href="https://example.org/second">Second &amp; result</a>
<a class="result__snippet" href="#">Snippet &lt;two&gt;</a>
</div>
</body></html>`

const bingFixture = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0"><channel><title>Bing: test</title>
<item><title>Go 1.27 Release Notes</title><link>https://go.dev/doc/go1.27</link><description>&lt;p&gt;Changes to Go &amp; tools&lt;/p&gt;</description></item>
<item><title>Download</title><link>https://go.dev/dl/</link><description>Get Go 1.27</description></item>
</channel></rss>`

func TestParseDDGHTML(t *testing.T) {
	res, err := parseDDGHTML([]byte(ddgFixture), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(res), res)
	}
	if res[0].URL != "https://go.dev/blog/go1.27" {
		t.Fatalf("uddg redirect not unwrapped: %q", res[0].URL)
	}
	if res[0].Title != "Go 1.27 is released" {
		t.Fatalf("title = %q", res[0].Title)
	}
	if !strings.Contains(res[0].Snippet, "Go team") {
		t.Fatalf("snippet = %q", res[0].Snippet)
	}
	if res[1].Title != "Second & result" {
		t.Fatalf("entity not unescaped: %q", res[1].Title)
	}
	if res[1].Snippet != "Snippet <two>" {
		t.Fatalf("snippet tags/entities = %q", res[1].Snippet)
	}
}

func TestParseBingRSS(t *testing.T) {
	res, err := parseBingRSS([]byte(bingFixture), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d, want 2", len(res))
	}
	if res[0].URL != "https://go.dev/doc/go1.27" || res[0].Title != "Go 1.27 Release Notes" {
		t.Fatalf("bad first item: %+v", res[0])
	}
	if res[0].Snippet != "Changes to Go & tools" {
		t.Fatalf("snippet = %q", res[0].Snippet)
	}
}

// TestLiveSearch hits the real endpoints; skipped under -short.
func TestLiveSearch(t *testing.T) {
	if testing.Short() || os.Getenv("CI") != "" {
		t.Skip("live network test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := New()
	res, err := s.Search(ctx, "go 1.27 released", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("no results")
	}
	for _, r := range res {
		if r.Title == "" || r.URL == "" {
			t.Fatalf("incomplete result: %+v", r)
		}
		t.Logf("%s — %s", r.Title, r.URL)
	}
}
