// Package disk reports free space on the filesystem holding a path.
// Stdlib-only: Statfs on unix, GetDiskFreeSpaceEx on Windows.
package disk

// Space describes the filesystem that contains a path.
type Space struct {
	FreeBytes  int64 `json:"freeBytes"`
	TotalBytes int64 `json:"totalBytes"`
}
