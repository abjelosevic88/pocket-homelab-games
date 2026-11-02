//go:build !unix

package server

// diskFree is the space left on the disk holding path (-1 = unknown).
func diskFree(string) int64 { return -1 }
