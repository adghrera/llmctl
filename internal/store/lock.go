package store

import (
	"os"
	"time"
)

// fileLock is a tiny advisory lock built on O_CREATE|O_EXCL lockfiles with
// stale-lock breaking. Good enough for one daemon + interactive CLI calls
// on one machine; the state file itself is written atomically.
type fileLock struct {
	path string
	held *os.File
}

const (
	lockTimeout = 30 * time.Second
	staleAfter  = 60 * time.Second
)

func (l *fileLock) acquire() error {
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			l.held = f
			return nil
		}
		if !os.IsExist(err) {
			return err
		}
		// Break stale locks (crashed holder).
		if fi, serr := os.Stat(l.path); serr == nil && time.Since(fi.ModTime()) > staleAfter {
			os.Remove(l.path)
			continue
		}
		if time.Now().After(deadline) {
			return os.ErrDeadlineExceeded
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (l *fileLock) release() {
	if l.held != nil {
		l.held.Close()
		os.Remove(l.path)
		l.held = nil
	}
}
