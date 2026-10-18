package proxy

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-5AB0DC85B11"

// wsEchoServer is a minimal RFC6455 handshake responder that, after the 101,
// echoes raw bytes back. The proxy pipes bytes without parsing frames, so
// raw echo is sufficient to prove end-to-end passthrough.
func wsEchoServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ws" {
			http.Error(w, "not found", 404)
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		if key == "" {
			http.Error(w, "no ws key", 400)
			return
		}
		accept := wsAccept(key)
		hj := w.(http.Hijacker)
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
		buf.Flush()
		io.Copy(buf, conn) // echo raw bytes
		buf.Flush()
	}))
}

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestWebSocketPassthrough(t *testing.T) {
	fake := wsEchoServer(t)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	// Drive the gateway over a real TCP server so Hijack works.
	gw := httptest.NewUnstartedServer(srv.testHandler())
	gw.Start()
	defer gw.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(gw.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	fmt.Fprintf(conn, "GET /v1/ws?model=test-model HTTP/1.1\r\nHost: gateway\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", key)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != wsAccept(key) {
		t.Fatalf("bad Sec-WebSocket-Accept: %q", got)
	}

	// After the handshake the proxy pipes raw bytes; the fake echoes.
	payload := "hello-websocket-passthrough"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("echo read failed: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("echo = %q, want %q", buf, payload)
	}
}

func TestWebSocketRejectsNonUpgrade(t *testing.T) {
	fake := wsEchoServer(t)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	req := httptest.NewRequest("GET", "/v1/ws?model=test-model", nil)
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-upgrade GET /v1/ws, got %d: %s", rec.Code, rec.Body)
	}
}
