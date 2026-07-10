//go:build linux

package logging

import "golang.org/x/sys/unix"

func stdoutTerminalWidth(fd int) int {
	size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 {
		return 0
	}
	return int(size.Col)
}
