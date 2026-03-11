//go:build !windows

package tmux

import "syscall"

// execSyscall replaces the current process with the given command.
func execSyscall(bin string, args []string) error {
	return syscall.Exec(bin, args, syscall.Environ())
}
