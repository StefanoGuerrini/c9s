//go:build windows

package tmux

import "fmt"

func execSyscall(bin string, args []string) error {
	return fmt.Errorf("tmux is not supported on Windows")
}
