//go:build !unix

package trace

import "os/exec"

// OwnProcessGroup does nothing where process groups are not available.
func OwnProcessGroup(*exec.Cmd) {}

// TerminateGroup kills the command; its children may outlive it.
func TerminateGroup(cmd *exec.Cmd) error { return cmd.Process.Kill() }
