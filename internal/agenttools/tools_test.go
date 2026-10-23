package agenttools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCalc(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"expression":"2+3*4"}`, "14"},
		{`{"expression":"(2+3)*4"}`, "20"},
		{`{"expression":"2^10"}`, "1024"},
		{`{"expression":"100/8"}`, "12.5"},
		{`{"expression":"17 % 5"}`, "2"},
		{`{"expression":"-5+3"}`, "-2"},
		{`{"expression":"2^3^2"}`, "512"}, // right-assoc: 2^9
		{`{"expression":"1,234.5+0.5"}`, "1235"},
		{`{"expression":"0.1+0.2"}`, "0.30000000000000004"},
	}
	tool := &CalcTool{}
	for _, c := range cases {
		got, _, err := tool.Execute(context.Background(), c.in)
		if err != nil {
			t.Errorf("%s: err %v", c.in, err)
			continue
		}
		if !strings.HasSuffix(got, "= "+c.want) {
			t.Errorf("%s: got %q want suffix %q", c.in, got, c.want)
		}
	}
	// errors
	for _, bad := range []string{
		`{"expression":"1/0"}`,
		`{"expression":"(1+2"}`,
		`{"expression":"1+"}`,
		`{"expression":"abc"}`,
		`{"expression":""}`,
	} {
		if _, _, err := tool.Execute(context.Background(), bad); err == nil {
			t.Errorf("%s: expected error", bad)
		}
	}
}

func TestFetchURLTextExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><script>var x=1;</script><style>p{}</style></head>
				<body><h1>Hello Title</h1><p>First paragraph with &amp; entity.</p>
				<ul><li>alpha</li><li>beta</li></ul>
				<a href="https://example.com/x">link text</a></body></html>`)
		case "/err":
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	tool := &FetchURLTool{AllowPrivate: true} // test server is loopback
	text, results, err := tool.Execute(context.Background(), fmt.Sprintf(`{"url":"%s/page"}`, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello Title") || !strings.Contains(text, "First paragraph with & entity.") {
		t.Fatalf("text extraction wrong: %q", text)
	}
	if strings.Contains(text, "var x=1") || strings.Contains(text, "<p>") {
		t.Fatalf("script/tag leaked: %q", text)
	}
	if !strings.Contains(text, "link text (https://example.com/x)") {
		t.Fatalf("link href lost: %q", text)
	}
	if rs, ok := results.([]map[string]any); !ok || len(rs) != 1 {
		t.Fatalf("results: %v", results)
	}

	if _, _, err := tool.Execute(context.Background(), fmt.Sprintf(`{"url":"%s/err"}`, srv.URL)); err == nil {
		t.Error("HTTP 500 should error")
	}
}

func TestFetchURLSSRFGuard(t *testing.T) {
	tool := &FetchURLTool{}
	blocked := []string{
		`{"url":"http://127.0.0.1:8080/api/v1/keys"}`,
		`{"url":"http://localhost:8080/"}`,
		`{"url":"http://169.254.169.254/latest/meta-data/"}`, // cloud metadata
		`{"url":"http://10.0.0.5/"}`,                        // private
		`{"url":"http://192.168.1.1/"}`,                     // private
		`{"url":"http://[::1]/"}`,                           // ipv6 loopback
		`{"url":"http://100.64.0.1/"}`,                      // CGNAT
		`{"url":"file:///etc/passwd"}`,
		`{"url":"http://localhost.localdomain/"}`,
	}
	for _, b := range blocked {
		if _, _, err := tool.Execute(context.Background(), b); err == nil {
			t.Errorf("SSRF not blocked: %s", b)
		}
	}
	// empty url
	if _, _, err := tool.Execute(context.Background(), `{}`); err == nil {
		t.Error("empty url should error")
	}
}

func TestFetchURLRedirectGuard(t *testing.T) {
	// The default client's CheckRedirect must reject loopback targets even
	// when the initial URL was allowed (e.g. a public site that 302s to
	// metadata/loopback).
	tool := &FetchURLTool{}
	c := tool.client()
	redirectTo, _ := url.Parse("http://127.0.0.1:9999/secret")
	req := &http.Request{URL: redirectTo}
	via := []*http.Request{{URL: mustParse(t, "https://example.com/start")}}
	if err := c.CheckRedirect(req, via); err == nil {
		t.Error("redirect to loopback should be blocked")
	}
	// a public redirect target is allowed
	pub, _ := url.Parse("https://example.com/next")
	if err := c.CheckRedirect(&http.Request{URL: pub}, via); err != nil {
		t.Errorf("public redirect blocked: %v", err)
	}
	// too many redirects stops following
	long := []*http.Request{{}, {}, {}, {}, {}}
	if err := c.CheckRedirect(req, long); err != http.ErrUseLastResponse {
		t.Errorf("expected ErrUseLastResponse, got %v", err)
	}
}

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTimeTool(t *testing.T) {
	text, results, err := (&TimeTool{}).Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	rs, ok := results.([]map[string]any)
	if !strings.Contains(text, "UTC") || !ok || len(rs) != 1 {
		t.Fatalf("bad time output: %q %v", text, results)
	}
}
