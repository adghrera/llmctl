// Package scheduler manages scheduled agent tasks that run at configured
// intervals (cron-like) and save results as conversations.
package scheduler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"llmctl/internal/paths"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Spec is one scheduled task specification.
type Spec struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CronExpr    string    `json:"cron_expr"`    // cron expression (e.g. "0 9 * * *")
	Model       string    `json:"model"`        // model id to use
	Prompt      string    `json:"prompt"`       // prompt to send
	Enabled     bool      `json:"enabled"`      // whether the task is active
	LastRun     time.Time `json:"last_run"`     // last execution time
	NextRun     time.Time `json:"next_run"`     // next scheduled execution
	CreatedAt   time.Time `json:"created_at"`   // creation time
	UpdatedAt   time.Time `json:"updated_at"`   // last update time
}

// Record is a task spec with its on-disk directory.
type Record struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	Spec Spec   `json:"spec"`
}

// Store manages scheduled task specs on disk.
type Store struct {
	dir string // $LLMCTL_HOME/scheduler
}

// Open opens (creating if needed) the scheduler store.
func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "scheduler")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("scheduler dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the on-disk directory for a task id.
func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

// genID generates a random scheduler id.
func genID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "sched-000000"
	}
	return "sched-" + hex.EncodeToString(b)
}

// load reads one task dir.
func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, "spec.json"))
	if err != nil {
		return nil, fmt.Errorf("scheduler %s: %w", id, err)
	}
	var spec Spec
	if err := json.Unmarshal(mb, &spec); err != nil {
		return nil, fmt.Errorf("scheduler %s: bad spec: %w", id, err)
	}
	if spec.ID == "" {
		spec.ID = id
	}
	return &Record{
		ID:   spec.ID,
		Dir:  dir,
		Spec: spec,
	}, nil
}

// List returns all scheduled tasks sorted by created_at descending.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []Record{}
	for _, e := range entries {
		if !e.IsDir() || !safeID.MatchString(e.Name()) {
			continue
		}
		r, err := s.load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Spec.CreatedAt.After(out[j].Spec.CreatedAt)
	})
	return out, nil
}

// Get returns one task by id.
func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid schedule id")
	}
	return s.load(id)
}

// Create writes a new task directory and returns the record.
func (s *Store) Create(spec Spec) (*Record, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return nil, fmt.Errorf("schedule missing name")
	}
	if strings.TrimSpace(spec.CronExpr) == "" {
		return nil, fmt.Errorf("schedule missing cron_expr")
	}
	if strings.TrimSpace(spec.Model) == "" {
		return nil, fmt.Errorf("schedule missing model")
	}
	if strings.TrimSpace(spec.Prompt) == "" {
		return nil, fmt.Errorf("schedule missing prompt")
	}
	id := genID()
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("schedule %s already exists", id)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	spec.ID = id
	spec.CreatedAt = now
	spec.UpdatedAt = now
	spec.NextRun = nextRun(spec.CronExpr, now)
	if err := writeJSON(filepath.Join(final, "spec.json"), spec); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return s.load(id)
}

// Update replaces an existing task spec.
func (s *Store) Update(id string, spec Spec) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid schedule id")
	}
	rec, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	spec.ID = id
	spec.CreatedAt = rec.Spec.CreatedAt
	spec.UpdatedAt = time.Now().UTC()
	spec.NextRun = nextRun(spec.CronExpr, spec.UpdatedAt)
	if err := writeJSON(filepath.Join(rec.Dir, "spec.json"), spec); err != nil {
		return nil, err
	}
	return s.load(id)
}

// SetEnabled toggles a task's enabled state.
func (s *Store) SetEnabled(id string, on bool) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	rec.Spec.Enabled = on
	rec.Spec.UpdatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(rec.Dir, "spec.json"), rec.Spec); err != nil {
		return err
	}
	return nil
}

// Delete removes the task directory.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

// Due returns all enabled tasks whose next_run is in the past.
func (s *Store) Due() ([]Record, error) {
	now := time.Now().UTC()
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	var due []Record
	for _, r := range list {
		if !r.Spec.Enabled {
			continue
		}
		if r.Spec.NextRun.IsZero() || !r.Spec.NextRun.After(now) {
			due = append(due, r)
		}
	}
	return due, nil
}

// UpdateNextRun updates the next_run time for a task after execution.
func (s *Store) UpdateNextRun(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	rec.Spec.LastRun = now
	rec.Spec.NextRun = nextRun(rec.Spec.CronExpr, now)
	rec.Spec.UpdatedAt = now
	if err := writeJSON(filepath.Join(rec.Dir, "spec.json"), rec.Spec); err != nil {
		return err
	}
	return nil
}

// writeJSON writes a value as JSON to a file.
func writeJSON(path string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// nextRun computes the next run time after 'now' for a cron expression.
// Supports a simplified 5-field cron: "min hour day month weekday".
func nextRun(cronExpr string, now time.Time) time.Time {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return now.Add(time.Hour) // default: 1 hour
	}
	minField, hourField, dayField, monthField, weekdayField := fields[0], fields[1], fields[2], fields[3], fields[4]

	t := now.Add(time.Minute) // start 1 minute from now
	// Limit search to 366 days to avoid infinite loops
	for i := 0; i < 366*24*60; i++ {
		if matchesField(t.Minute(), minField) &&
			matchesField(t.Hour(), hourField) &&
			matchesField(t.Day(), dayField) &&
			matchesField(int(t.Month()), monthField) &&
			matchesWeekday(t.Weekday(), weekdayField) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return now.Add(24 * time.Hour) // fallback: tomorrow
}

// matchesField checks if a value matches a cron field.
// Supports: *, */N, N, N-M, N,M,O.
func matchesField(val int, field string) bool {
	if field == "*" {
		return true
	}
	if strings.HasPrefix(field, "*/") {
		step, err := parseInt(field[2:])
		if err != nil || step == 0 {
			return false
		}
		return val%step == 0
	}
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) == 2 {
			lo, err1 := parseInt(parts[0])
			hi, err2 := parseInt(parts[1])
			if err1 == nil && err2 == nil && lo <= val && val <= hi {
				return true
			}
		}
	}
	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		for _, p := range parts {
			if matchesField(val, p) {
				return true
			}
		}
	}
	v, err := parseInt(field)
	if err == nil && v == val {
		return true
	}
	return false
}

// matchesWeekday checks if a weekday matches a cron weekday field.
// Sunday = 0 in time.Weekday, but cron uses 0 = Sunday too.
func matchesWeekday(wd time.Weekday, field string) bool {
	return matchesField(int(wd), field)
}

// parseInt parses an integer string.
func parseInt(s string) (int, error) {
	var neg bool
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}
