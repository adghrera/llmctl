// Package backup exports and restores llmctl's state directory as a single
// tar.gz archive: configuration, chats, knowledge bases, memory, skills,
// tools, scheduler jobs, and plugins. Heavy or regenerable content
// (backend binaries, model files, logs) is excluded.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"llmctl/internal/paths"
)

// excludeDirs are top-level entries never included in a backup.
var excludeDirs = map[string]bool{
	"backends": true, // installed binaries (re-installable)
	"models":   true, // GGUF files (re-downloadable)
	"logs":     true, // instance logs
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
		_, err = io.Copy(tw, f)
		return err
	})
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
