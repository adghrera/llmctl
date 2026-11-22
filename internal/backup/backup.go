// Package backup exports and restores llmctl's state directory as a single
// tar.gz archive: configuration, chats, knowledge bases, memory, skills,
// tools, scheduler jobs, and plugins. Heavy or regenerable content
// (backend binaries, model files, logs) is excluded.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"llmctl/internal/paths"
)

// excludeDirs are top-level entries never included in a backup.
var excludeDirs = map[string]bool{
	"backends": true, // installed binaries (re-installable)
	"models":   true, // GGUF files (re-downloadable)
	"logs":     true, // instance logs
	"backups":  true, // scheduled snapshots (would nest / grow during write)
}

// excludeFiles are top-level files never included.
var excludeFiles = map[string]bool{
	"llmctl.log": true,
}

// Create writes a tar.gz of the state directory to w.
func Create(w io.Writer) error {
	home := paths.Home()
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if info.IsDir() && excludeDirs[top] {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			if rel == top && excludeFiles[top] {
				return nil
			}
			if strings.HasSuffix(rel, ".lock") {
				return nil
			}
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if strings.HasSuffix(rel, ".lock") {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.ModTime = time.Time{} // deterministic archive
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		// Trust the size of the open handle and copy exactly that many bytes,
		// so a file that grows during the walk cannot overflow the header.
		if fi, err := f.Stat(); err == nil {
			hdr.Size = fi.Size()
		}
		n, err := io.CopyN(tw, f, hdr.Size)
		if err != nil && err != io.EOF {
			return err
		}
		if n < hdr.Size {
			// File shrank mid-copy: pad so the entry matches its header.
			if _, err := io.CopyN(tw, io.LimitReader(zeroReader{}, hdr.Size-n), hdr.Size-n); err != nil {
				return err
			}
		}
		return nil
	})
}

// zeroReader yields infinite zero bytes (tar padding).
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// Restore extracts a tar.gz archive into the state directory and returns the
// number of files written. Existing files with the same names are overwritten;
// other files are left in place.
func Restore(r io.Reader) (int, error) {
	home := paths.Home()
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("backup: not a gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		name := filepath.Clean(filepath.FromSlash(hdr.Name))
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return count, fmt.Errorf("backup: unsafe path in archive: %q", hdr.Name)
		}
		top := strings.SplitN(filepath.ToSlash(name), "/", 2)[0]
		if excludeDirs[top] {
			continue // never restore binaries/models/logs
		}
		target := filepath.Join(home, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return count, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return count, err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode).Perm())
			if err != nil {
				return count, err
			}
			// Cap per-file size defensively (1 GiB).
			n, err := io.Copy(f, io.LimitReader(tr, 1<<30))
			closeErr := f.Close()
			if err != nil {
				return count, err
			}
			if closeErr != nil {
				return count, closeErr
			}
			if n == 1<<30 {
				return count, fmt.Errorf("backup: file too large: %s", hdr.Name)
			}
			count++
		}
	}
	return count, nil
}

// CreateSnapshot writes a timestamped snapshot (llmctl-[label-]YYYYMMDD-HHMMSS.tar.gz)
// into dir and prunes old ones, keeping the newest keep. An optional label
// (F443) is sanitized and embedded before the timestamp. Returns the file name
// and size. Shared by the scheduler backup kind (F077) and the on-demand
// POST /api/v1/backups endpoint (F088).
// Verify checks a snapshot file's integrity (F242): gzip + tar readable, no
// unsafe paths, and state.json present and valid JSON. Returns entry count.
func Verify(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("backup: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("backup: not a gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	sawState := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("backup: corrupt archive after %d entries: %w", count, err)
		}
		name := filepath.Clean(filepath.FromSlash(hdr.Name))
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return count, fmt.Errorf("backup: unsafe path in archive: %q", hdr.Name)
		}
		if hdr.Typeflag == tar.TypeReg {
			count++
			if filepath.ToSlash(hdr.Name) == "state.json" {
				sawState = true
				var probe map[string]any
				if err := json.NewDecoder(tr).Decode(&probe); err != nil {
					return count, fmt.Errorf("backup: state.json is not valid JSON: %w", err)
				}
			}
		}
	}
	if !sawState {
		return count, fmt.Errorf("backup: no state.json in archive")
	}
	return count, nil
}

// sanitizeLabel keeps a backup label file-safe: only letters, digits, dot,
// dash and underscore, max 40 chars (F443).
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if len(out) > 40 {
		out = out[:40]
	}
	return strings.Trim(out, "-._")
}

func CreateSnapshot(dir string, keep int, label string) (string, int64, error) {
	if keep <= 0 {
		keep = 7
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("backups dir: %w", err)
	}
	name := "llmctl-"
	if l := sanitizeLabel(label); l != "" { // F443: optional label in the file name
		name += l + "-"
	}
	name += time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", 0, fmt.Errorf("create backup: %w", err)
	}
	if err := Create(f); err != nil {
		f.Close()
		os.Remove(path)
		return "", 0, fmt.Errorf("backup write: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", 0, fmt.Errorf("close backup: %w", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "llmctl-*.tar.gz"))
	// Oldest-first by mtime: labels make names non-chronological (F558).
	sort.Slice(matches, func(i, j int) bool {
		fi, err := os.Stat(matches[i])
		if err != nil {
			return true
		}
		fj, err := os.Stat(matches[j])
		if err != nil {
			return false
		}
		if !fi.ModTime().Equal(fj.ModTime()) {
			return fi.ModTime().Before(fj.ModTime())
		}
		return matches[i] < matches[j]
	})
	for len(matches) > keep {
		if err := os.Remove(matches[0]); err == nil {
			matches = matches[1:]
		} else {
			break
		}
	}
	fi, _ := os.Stat(path)
	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}
	return name, size, nil
}
