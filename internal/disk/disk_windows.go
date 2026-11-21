//go:build windows

package disk

import (
	"syscall"
	"unsafe"
)

// Free returns free/total bytes of the filesystem containing path.
// GetDiskFreeSpaceExW is resolved via kernel32 (not exported by stdlib syscall).
func Free(path string) (Space, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return Space{}, err
	}
	var free, total, totalFree uint64
	r, _, e := proc.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)))
	if r == 0 {
		return Space{}, e
	}
	return Space{FreeBytes: int64(free), TotalBytes: int64(total)}, nil
}
