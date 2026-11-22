package api

import (
	"strings"
	"testing"

	"llmctl/internal/disk"
)

// F236: the state check warns when the state-dir filesystem is nearly full.
func TestStateSpaceCheck(t *testing.T) {
	cases := []struct {
		name       string
		free       int64
		wantStatus string
		wantSub    string
	}{
		{"plenty", 60 << 30, "ok", "60.0 GiB free"},
		{"just over", 1<<30 + 1, "ok", "1.0 GiB free"},
		{"under 1 GiB", 512 << 20, "warn", "0.50 GiB free"},
		{"empty", 0, "warn", "0.00 GiB free"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, detail := stateSpaceCheck("/home/x", disk.Space{FreeBytes: c.free})
			if status != c.wantStatus {
				t.Errorf("status = %q, want %q (%s)", status, c.wantStatus, detail)
			}
			if !strings.Contains(detail, c.wantSub) {
				t.Errorf("detail = %q, want substring %q", detail, c.wantSub)
			}
		})
	}
}
