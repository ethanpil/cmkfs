//go:build linux

package executor

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in its own process group so a stray Ctrl+C at
// the terminal doesn't signal mkfs directly (spec §11).
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	// Negative pid signals the whole process group.
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}

func terminateGroup(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGTERM) }
func killGroup(cmd *exec.Cmd)      { signalGroup(cmd, syscall.SIGKILL) }
