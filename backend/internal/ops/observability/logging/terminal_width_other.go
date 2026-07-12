//go:build !linux

package logging

func stdoutTerminalWidth(int) int {
	return 0
}
