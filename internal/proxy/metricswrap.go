package proxy

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
)

// recordingResponseWriter passes every byte straight through (streaming is
// never delayed) while teeing up to cap bytes into a buffer so usage fields
// can be scanned after the fact.
type recordingResponseWriter struct {
	http.ResponseWriter
	status  int
	buf     bytes.Buffer
	cap     int
	written bool
}

func newRecordingResponseWriter(w http.ResponseWriter) *recordingResponseWriter {
	return &recordingResponseWriter{ResponseWriter: w, status: 200, cap: 1 << 20}
}

func (w *recordingResponseWriter) WriteHeader(code int) {
	if !w.written {
		w.status = code
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.written = true // implicit 200
	}
	if w.buf.Len() < w.cap {
		remain := w.cap - w.buf.Len()
		if len(b) <= remain {
			w.buf.Write(b)
		} else {
			w.buf.Write(b[:remain])
		}
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying Flusher so SSE keeps streaming.
func (w *recordingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying Hijacker so WebSocket upgrades work
// through the metrics wrapper.
func (w *recordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *recordingResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ---- model ref plumbing ----

// modelRef is a mutable holder the route handler fills in with the peeked
// model/stream flags, so the metrics middleware (which wraps the mux from
// the outside) can attribute the request without re-parsing the body.
type modelRef struct {
	Model  string
	Stream bool
	Key    string // API key prefix (first 8 chars) for per-key cost tracking
}

type modelRefKey struct{}

func withModelRef(ctx context.Context, ref *modelRef) context.Context {
	return context.WithValue(ctx, modelRefKey{}, ref)
}

func modelRefFrom(ctx context.Context) *modelRef {
	if ref, ok := ctx.Value(modelRefKey{}).(*modelRef); ok {
		return ref
	}
	return nil
}
