package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// handleWS proxies a WebSocket (or any HTTP Upgrade) connection end to end:
// it performs the upgrade handshake against the chosen instance on the
// client's behalf, then pipes raw bytes both ways until either side closes.
//
// Route: GET /v1/ws?model=<name>  (model optional when one instance runs;
// additional path/query is forwarded to the instance under /v1/ws).
//
// This is transport-agnostic beyond the Upgrade handshake — it works for
// RFC 6455 websockets and vendor "websocket-over-HTTP" variants alike.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		!headerHasToken(r.Header.Get("Connection"), "upgrade") {
		writeErr(w, http.StatusBadRequest, "invalid_request_error",
			"expected an Upgrade: websocket request")
		return
	}
	model := r.URL.Query().Get("model")
	target, err := s.router.Resolve(model)
	if err != nil {
		status, code := routeError(err)
		writeErr(w, status, code, err.Error())
		return
	}
	if target.Transport != "http" {
		writeErr(w, http.StatusNotImplemented, "not_implemented",
			"websocket passthrough requires an http-transport instance")
		return
	}

	upstreamAddr := target.BaseURL.Host
	upstreamPath := joinPath(target.APIPath, strings.TrimPrefix(r.URL.Path, "/v1"))
	if upstreamPath == "" {
		upstreamPath = "/ws"
	}
	if r.URL.RawQuery != "" {
		q := r.URL.Query()
		q.Del("model")
		if enc := q.Encode(); enc != "" {
			upstreamPath += "?" + enc
		}
	}

	s.logger.Info("ws upgrade: proxying", "instance", target.InstanceID, "upstream", upstreamAddr, "path", upstreamPath)

	dialer := &net.Dialer{Timeout: 15 * time.Second}
	upConn, err := dialer.DialContext(r.Context(), "tcp", upstreamAddr)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "api_error",
			fmt.Sprintf("upstream %s unavailable: %v", upstreamAddr, err))
		return
	}
	defer upConn.Close()

	// 1. Forward the client's handshake to the upstream.
	if err := writeUpgradeRequest(upConn, upstreamAddr, upstreamPath, r); err != nil {
		writeErr(w, http.StatusBadGateway, "api_error", "write upgrade: "+err.Error())
		return
	}

	// 2. Read the upstream's handshake response.
	ur := bufio.NewReader(upConn)
	resp, err := http.ReadResponse(ur, &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: upstreamPath},
		Header: http.Header{},
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "api_error", "read upstream handshake: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		// Upstream refused the upgrade: relay its response verbatim.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// 3. Hijack the client connection so we can write the raw 101 and pipe.
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeErr(w, http.StatusNotImplemented, "api_error", "server cannot hijack connections")
		return
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "api_error", "hijack: "+err.Error())
		return
	}
	defer clientConn.Close()

	// 4. Relay the 101 handshake response to the client.
	resp.Write(clientBuf)
	if err := clientBuf.Flush(); err != nil {
		return
	}

	// 5. Pipe both directions until EOF/error. clientBuf may hold buffered
	// bytes the client already sent after its handshake.
	var wg sync.WaitGroup
	pipe := func(dst net.Conn, src io.Reader) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, rerr := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if rerr != nil {
				break
			}
		}
		closeWrite(dst)
	}
	wg.Add(2)
	go pipe(upConn, io.MultiReader(clientBuf, clientConn))
	go pipe(clientConn, ur)
	wg.Wait()
}

// writeUpgradeRequest writes a minimal HTTP/1.1 upgrade request to the
// upstream, copying the client's handshake headers (minus hop-by-hop ones).
func writeUpgradeRequest(c net.Conn, host, path string, r *http.Request) error {
	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&b, "Host: %s\r\n", host)
	for _, name := range []string{"Upgrade", "Connection", "Sec-WebSocket-Key",
		"Sec-WebSocket-Version", "Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions"} {
		for _, v := range r.Header.Values(name) {
			fmt.Fprintf(&b, "%s: %s\r\n", name, v)
		}
	}
	// Forward auth to the gateway through to the instance only if the
	// instance itself uses keys (rare locally); skip Authorization by default.
	b.WriteString("\r\n")
	_, err := io.WriteString(c, b.String())
	return err
}

func headerHasToken(v, token string) bool {
	for _, part := range strings.Split(v, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// closeWrite half-closes a conn when possible (TCP), signalling EOF upstream.
func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		tc.CloseWrite()
		return
	}
	if tc, ok := c.(interface{ CloseWrite() error }); ok {
		tc.CloseWrite()
	}
}
