package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"llmctl/internal/backend"
	"llmctl/internal/jobs"
	"llmctl/internal/store"
)

func newJobsTestServer(t *testing.T) (*httptest.Server, *jobs.Manager) {
	t.Helper()
	t.Setenv("LLMCTL_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := backend.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	jm := jobs.NewManager()
	srv := New(st, reg, nil, jm, slog.New(slog.NewTextHandler(io.Discard, nil)), 0, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, jm
}

// F575: GET /api/v1/jobs/{id} returns one job; unknown ids are 404; the
// static /jobs/events route still wins over the {id} wildcard.
func TestGetJobByID(t *testing.T) {
	ts, jm := newJobsTestServer(t)

	release := make(chan struct{})
	jm.Submit("model-install", "tiny", func(ctx context.Context, j *jobs.Job) error {
		j.SetProgress(0.5, "halfway")
		<-release
		return nil
	})
	// Find the submitted job id via the list endpoint.
	resp, err := http.Get(ts.URL + "/api/v1/jobs")
	if err != nil {
		t.Fatal(err)
	}
	var list []struct {
		ID string `json:"id"`
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := json.Unmarshal(body, &list); err != nil || len(list) != 1 {
		t.Fatalf("expected one job, got %s", body)
	}
	id := list[0].ID

	// Running job: 200 with progress.
	resp, err = http.Get(ts.URL + "/api/v1/jobs/" + id)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var j struct {
		ID       string  `json:"id"`
		Status   string  `json:"status"`
		Progress float64 `json:"progress"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		t.Fatal(err)
	}
	if j.ID != id || j.Status != "running" {
		t.Fatalf("unexpected job: %s", body)
	}

	// Let it finish; then it reports done.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err = http.Get(ts.URL + "/api/v1/jobs/" + id)
		if err != nil {
			t.Fatal(err)
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err := json.Unmarshal(body, &j); err == nil && j.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never reached done: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Unknown id: 404.
	resp, err = http.Get(ts.URL + "/api/v1/jobs/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("unknown job status = %d, want 404", resp.StatusCode)
	}

	// The static events route must not be shadowed by the {id} wildcard.
	cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, "GET", ts.URL+"/api/v1/jobs/events", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("events content-type = %q", ct)
	}
}

// F585: DELETE /api/v1/jobs drops finished jobs and keeps running ones.
func TestClearJobs(t *testing.T) {
	ts, jm := newJobsTestServer(t)

	release := make(chan struct{})
	jm.Submit("model-install", "stuck", func(ctx context.Context, j *jobs.Job) error {
		<-release
		return nil
	})
	jm.Submit("model-install", "quick", func(ctx context.Context, j *jobs.Job) error {
		return nil
	})
	// Wait for the quick job to finish.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(jm.List()) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("jobs never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	done := false
	for {
		for _, j := range jm.List() {
			if j.Target == "quick" && j.Status == "done" {
				done = true
			}
		}
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !done {
		t.Fatal("quick job never reached done")
	}

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/jobs", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var res struct {
		Cleared int `json:"cleared"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Cleared != 1 {
		t.Fatalf("expected cleared=1, got %s", body)
	}
	// Only the running job remains.
	resp, err = http.Get(ts.URL + "/api/v1/jobs")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var list []struct {
		Target string `json:"target"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list) != 1 || list[0].Target != "stuck" {
		t.Fatalf("expected only the running job, got %s", body)
	}
	close(release)
}
