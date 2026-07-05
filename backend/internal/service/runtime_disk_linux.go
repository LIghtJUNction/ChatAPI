//go:build linux

package service

import (
	"os"
	"strings"
	"syscall"
)

func readDiskUsage(path string) (uint64, uint64) {
	if strings.TrimSpace(path) == "" {
		return 0, 0
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return 0, 0
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, 0
	}
	blockSize := uint64(stats.Bsize)
	return stats.Blocks * blockSize, stats.Bavail * blockSize
}
