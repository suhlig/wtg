//go:build unix

package trace

import (
	"os/exec"
	"syscall"
)

// OwnProcessGroup puts the command and everything it spawns (git's transport
// helpers, a shell's children) in a process group of their own, so they can be
// stopped together with TerminateGroup. The terminal's Ctrl-C no longer reaches
// them, so whoever sets this must stop them itself on interrupt.
func OwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// TerminateGroup asks the command and its whole process group to stop with
// SIGTERM, which lets git remove its temporary pack. It is meant for
// exec.Cmd.Cancel on a command started with OwnProcessGroup.
func TerminateGroup(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
