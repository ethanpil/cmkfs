//go:build !linux

package executor

import "os/exec"

// cmkfs only runs on Linux (spec §2); these stubs exist so the package
// compiles everywhere for development and unit tests.

func setProcAttr(cmd *exec.Cmd) {}

func terminateGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
