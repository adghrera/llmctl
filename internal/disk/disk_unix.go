//go:build !windows

package disk

import "syscall"

// Free returns free/total bytes of the filesystem containing path.
func Free(path string) (Space, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Space{}, err
	}
	return Space{
		FreeBytes:  int64(st.Bavail) * int64(st.Bsize),
		TotalBytes: int64(st.Blocks) * int64(st.Bsize),
	}, nil
}
