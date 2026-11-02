//go:build unix

package server

import "syscall"

// diskFree is the space left on the disk holding path (-1 = unknown).
func diskFree(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
