//go:build integration

package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// buildBinary compiles the shipped cmkfs binary once per test run.
var binaryPath string

func buildBinary(t *testing.T) string {
	t.Helper()
	if binaryPath != "" {
		return binaryPath
	}
	// Allow running the CLI tests against a prebuilt binary on a target that
	// has no Go toolchain or source tree (e.g. a bare test VM): set
	// CMKFS_BIN=/path/to/cmkfs. The path must be executable by an unprivileged
	// user, since TestCLIExitNotRoot re-execs it as nobody.
	if bin := os.Getenv("CMKFS_BIN"); bin != "" {
		binaryPath = bin
		return binaryPath
	}
	out := filepath.Join(os.TempDir(), "cmkfs-integration-test")
	cmd := exec.Command("go", "build", "-o", out, "github.com/ethanpil/cmkfs/cmd/cmkfs")
	cmd.Dir = ".."
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, msg)
	}
	binaryPath = out
	return out
}

func exitCode(t *testing.T, cmd *exec.Cmd) (int, string) {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("cannot run %v: %v", cmd.Args, err)
	return -1, ""
}

// TestCLIVersion: --version needs no root and no TTY (spec §12).
func TestCLIVersion(t *testing.T) {
	bin := buildBinary(t)
	code, out := exitCode(t, exec.Command(bin, "--version"))
	if code != 0 {
		t.Fatalf("--version exit %d: %s", code, out)
	}
	for _, want := range []string{"cmkfs", "ext4", "xfs", "btrfs"} {
		if !strings.Contains(out, want) {
			t.Errorf("--version output missing %q: %s", want, out)
		}
	}
}

// TestCLIExitUsage: unknown device path argument -> exit 2.
func TestCLIExitUsage(t *testing.T) {
	requireRoot(t)
	bin := buildBinary(t)
	code, out := exitCode(t, exec.Command(bin, "/dev/nonexistent-cmkfs-test"))
	if code != 2 {
		t.Fatalf("want exit 2, got %d: %s", code, out)
	}

	// Bad flag -> exit 2 as well.
	code, _ = exitCode(t, exec.Command(bin, "--no-such-flag"))
	if code != 2 {
		t.Fatalf("bad flag: want exit 2, got %d", code)
	}
}

// TestCLIExitEnv: PATH without lsblk -> exit 3.
func TestCLIExitEnv(t *testing.T) {
	requireRoot(t)
	bin := buildBinary(t)
	cmd := exec.Command(bin)
	cmd.Env = []string{"PATH=/nonexistent"}
	code, out := exitCode(t, cmd)
	if code != 3 {
		t.Fatalf("want exit 3, got %d: %s", code, out)
	}
	if !strings.Contains(out, "util-linux") {
		t.Errorf("exit-3 message must mention util-linux: %s", out)
	}
}

// TestCLIExitNotRoot: run as nobody -> exit 4.
func TestCLIExitNotRoot(t *testing.T) {
	requireRoot(t)
	bin := buildBinary(t)
	cmd := exec.Command(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 65534, Gid: 65534},
	}
	code, out := exitCode(t, cmd)
	if code != 4 {
		t.Fatalf("want exit 4, got %d: %s", code, out)
	}
	if !strings.Contains(out, "root") {
		t.Errorf("exit-4 message must mention root: %s", out)
	}
}

// TestCLIExitBlocked: a mounted positional device -> exit 5 with the finding.
func TestCLIExitBlocked(t *testing.T) {
	requireRoot(t)
	requireBinary(t, "mkfs.ext4")
	bin := buildBinary(t)
	loop := makeLoop(t, "1G")
	if out, err := exec.Command("mkfs.ext4", "-q", loop).CombinedOutput(); err != nil {
		t.Fatalf("mkfs: %v: %s", err, out)
	}
	mnt := t.TempDir()
	if out, err := exec.Command("mount", loop, mnt).CombinedOutput(); err != nil {
		t.Fatalf("mount: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("umount", mnt).Run() })

	code, out := exitCode(t, exec.Command(bin, "--show-loop", loop))
	if code != 5 {
		t.Fatalf("want exit 5, got %d: %s", code, out)
	}
	if !strings.Contains(out, "mounted") {
		t.Errorf("exit-5 output must include the finding: %s", out)
	}
}
