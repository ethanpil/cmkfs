//go:build integration

// Package integration exercises the real discover/safety/cmdgen/executor
// pipeline against loop devices (spec §13.2). Requires root and Linux; run
// with: sudo go test -tags integration ./integration/...
package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ethanpil/cmkfs/internal/cmdgen"
	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/executor"
	"github.com/ethanpil/cmkfs/internal/safety"
	"github.com/ethanpil/cmkfs/internal/schema"
)

func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("integration tests require root")
	}
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
}

// makeLoop creates a sparse backing file and attaches it to a free loop
// device; cleanup detaches and removes it.
func makeLoop(t *testing.T, size string) string {
	t.Helper()
	requireRoot(t)
	f := filepath.Join(t.TempDir(), "img")
	if out, err := exec.Command("truncate", "-s", size, f).CombinedOutput(); err != nil {
		t.Fatalf("truncate: %v: %s", err, out)
	}
	out, err := exec.Command("losetup", "--find", "--show", f).Output()
	if err != nil {
		t.Fatalf("losetup: %v", err)
	}
	dev := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		_ = exec.Command("losetup", "-d", dev).Run()
	})
	// Loop numbers get reused across tests; without settling, lsblk can
	// briefly report the previous attachment's filesystem signature.
	settleUdev()
	return dev
}

// settleUdev waits for udev to finish processing device events, so lsblk
// (which reads the udev database) reflects the device's current state.
func settleUdev() {
	_ = exec.Command("udevadm", "settle", "--timeout", "10").Run()
}

func schemaByID(t *testing.T, id string) schema.Schema {
	t.Helper()
	for _, s := range schema.Schemas {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no schema %s", id)
	return schema.Schema{}
}

func discoverLoop(t *testing.T, path string) (device.Device, []device.Device) {
	t.Helper()
	devs, err := device.Discover(true)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, d := range devs {
		if d.Path == path {
			return d, devs
		}
	}
	t.Fatalf("loop device %s not enumerated", path)
	return device.Device{}, nil
}

func realSystem() safety.System {
	return safety.System{ProbeExcl: safety.ProbeExclusive}
}

func blkid(t *testing.T, dev, tag string) string {
	t.Helper()
	out, err := exec.Command("blkid", "-o", "value", "-s", tag, dev).Output()
	if err != nil {
		return "" // no signature
	}
	return strings.TrimSpace(string(out))
}

// runPipeline formats a loop device through the full real pipeline and
// returns the terminal executor event.
func runPipeline(t *testing.T, s schema.Schema, values map[string]any, extra []string, loopDev string) executor.Event {
	t.Helper()
	target, _ := discoverLoop(t, loopDev)
	sys := realSystem()

	report := sys.Check(safety.Params{
		Device: target, All: nil, MinSizeBytes: s.MinSizeBytes, FSName: s.Name,
		ExtraArgs: len(extra), Probe: true,
	})
	if report.Blocked() {
		t.Fatalf("unexpected blockers: %+v", report.Findings)
	}

	argv, display, err := cmdgen.Build(s, values, extra, loopDev, report.NeedsForce(), report.IsWholeDisk())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Logf("running: %s", display)

	gate := safety.Gate{
		Sys: sys, Discover: device.Discover, ShowLoop: true,
		MinSizeBytes: s.MinSizeBytes, FSName: s.Name, ExtraArgs: len(extra),
	}
	fp := safety.FingerprintOf(target)
	gateFn := func() (safety.Report, bool) { return gate.FinalGate(loopDev, report, fp) }

	var done executor.Event
	for ev := range executor.Run(context.Background(), argv, gateFn) {
		if ev.Line != "" {
			t.Log(ev.Line)
		}
		if ev.Done {
			done = ev
		}
	}
	return done
}

func TestPipelineFormatEachFilesystem(t *testing.T) {
	cases := []struct {
		schemaID, label, wantFSType string
	}{
		{"ext4", "itext4", "ext4"},
		{"xfs", "itxfs", "xfs"},
		{"btrfs", "itbtrfs", "btrfs"},
		{"vfat", "ITVFAT", "vfat"}, // FAT labels are uppercase-only
		{"exfat", "itexfat", "exfat"},
	}
	for _, tc := range cases {
		t.Run(tc.schemaID, func(t *testing.T) {
			s := schemaByID(t, tc.schemaID)
			requireBinary(t, s.Binary)
			loop := makeLoop(t, "2G")
			label := tc.label

			done := runPipeline(t, s, map[string]any{"label": label}, nil, loop)
			if done.Exit != 0 || done.Aborted || done.Gate != nil {
				t.Fatalf("format failed: %+v", done)
			}
			if got := blkid(t, loop, "TYPE"); got != tc.wantFSType {
				t.Errorf("blkid TYPE = %q, want %q", got, tc.wantFSType)
			}
			if got := blkid(t, loop, "LABEL"); got != label {
				t.Errorf("blkid LABEL = %q, want %q", got, label)
			}
			if got := blkid(t, loop, "UUID"); got == "" {
				t.Error("blkid UUID empty after format")
			}
		})
	}
}

// TestExt4Largefile4InodeDensity: usage_type=largefile4 gives ~1 inode per
// 4 MiB (acceptance criterion 7).
func TestExt4Largefile4InodeDensity(t *testing.T) {
	requireBinary(t, "mkfs.ext4")
	requireBinary(t, "dumpe2fs")
	loop := makeLoop(t, "2G")

	done := runPipeline(t, schemaByID(t, "ext4"), map[string]any{"usage_type": "largefile4"}, nil, loop)
	if done.Exit != 0 {
		t.Fatalf("format failed: %+v", done)
	}
	out, err := exec.Command("dumpe2fs", "-h", loop).Output()
	if err != nil {
		t.Fatalf("dumpe2fs: %v", err)
	}
	var inodes int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Inode count:") {
			inodes, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Inode count:")))
		}
	}
	want := 2 * 1024 * 1024 * 1024 / (4 * 1024 * 1024) // size / 4 MiB = 512
	if inodes < want*9/10 || inodes > want*11/10 {
		t.Errorf("inode count %d not within ±10%% of %d", inodes, want)
	}
}

func TestExtraArgsPassthrough(t *testing.T) {
	requireBinary(t, "mkfs.ext4")
	loop := makeLoop(t, "1G")
	s := schemaByID(t, "ext4")

	// Assert argv placement first (before the device, after schema flags).
	argv, _, err := cmdgen.Build(s, map[string]any{"label": "x"}, []string{"-E", "nodiscard"}, loop, false, false)
	if err != nil {
		t.Fatal(err)
	}
	n := len(argv)
	if argv[n-1] != loop || argv[n-2] != "nodiscard" || argv[n-3] != "-E" {
		t.Fatalf("extra args misplaced: %v", argv)
	}

	done := runPipeline(t, s, map[string]any{"label": "x"}, []string{"-E", "nodiscard"}, loop)
	if done.Exit != 0 {
		t.Fatalf("format with extra args failed: %+v", done)
	}
}

func TestBlockerMounted(t *testing.T) {
	requireBinary(t, "mkfs.ext4")
	loop := makeLoop(t, "1G")
	if out, err := exec.Command("mkfs.ext4", "-q", loop).CombinedOutput(); err != nil {
		t.Fatalf("mkfs: %v: %s", err, out)
	}
	mnt := t.TempDir()
	if out, err := exec.Command("mount", loop, mnt).CombinedOutput(); err != nil {
		t.Fatalf("mount: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("umount", mnt).Run() })

	target, all := discoverLoop(t, loop)
	r := realSystem().Check(safety.Params{Device: target, All: all})
	if !r.Has("MOUNTED") || !r.Blocked() {
		t.Fatalf("expected MOUNTED blocker: %+v", r.Findings)
	}
}

func TestBlockerActiveSwap(t *testing.T) {
	requireBinary(t, "mkswap")
	loop := makeLoop(t, "256M")
	if out, err := exec.Command("mkswap", loop).CombinedOutput(); err != nil {
		t.Fatalf("mkswap: %v: %s", err, out)
	}
	if out, err := exec.Command("swapon", loop).CombinedOutput(); err != nil {
		t.Skipf("swapon not permitted here: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("swapoff", loop).Run() })

	target, all := discoverLoop(t, loop)
	r := realSystem().Check(safety.Params{Device: target, All: all})
	if !r.Has("ACTIVE_SWAP") {
		t.Fatalf("expected ACTIVE_SWAP: %+v", r.Findings)
	}
}

func TestBlockerDeviceBusy(t *testing.T) {
	loop := makeLoop(t, "256M")
	// Hold an O_EXCL fd from the test process.
	fd, err := syscall.Open(loop, syscall.O_RDONLY|syscall.O_EXCL, 0)
	if err != nil {
		t.Fatalf("cannot hold O_EXCL: %v", err)
	}
	defer syscall.Close(fd)

	target, all := discoverLoop(t, loop)
	r := realSystem().Check(safety.Params{Device: target, All: all, Probe: true})
	if !r.Has("DEVICE_BUSY") {
		t.Fatalf("expected DEVICE_BUSY: %+v", r.Findings)
	}
}

func TestBlockerInUseHolder(t *testing.T) {
	requireBinary(t, "dmsetup")
	loop := makeLoop(t, "256M")
	name := fmt.Sprintf("cmkfs-test-%d", os.Getpid())
	table := fmt.Sprintf("0 262144 linear %s 0", loop)
	if out, err := exec.Command("dmsetup", "create", name, "--table", table).CombinedOutput(); err != nil {
		t.Skipf("dmsetup create failed: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("dmsetup", "remove", name).Run() })

	target, all := discoverLoop(t, loop)
	r := realSystem().Check(safety.Params{Device: target, All: all})
	if !r.Has("IN_USE_HOLDER") {
		t.Fatalf("expected IN_USE_HOLDER: %+v", r.Findings)
	}
}

// TestFinalGateRace: the device changes between confirmation and execution;
// the gate must abort and nothing must be spawned (acceptance criterion 10).
func TestFinalGateRace(t *testing.T) {
	requireBinary(t, "mkfs.ext4")
	loop := makeLoop(t, "1G")
	s := schemaByID(t, "ext4")
	sys := realSystem()

	target, all := discoverLoop(t, loop)
	confirmed := sys.Check(safety.Params{Device: target, All: all, Probe: true})
	fp := safety.FingerprintOf(target) // captured while the device was blank

	// Someone formats the device while the confirm screen sits open.
	if out, err := exec.Command("mkfs.ext4", "-q", loop).CombinedOutput(); err != nil {
		t.Fatalf("mkfs: %v: %s", err, out)
	}
	settleUdev() // the change must be visible to lsblk before the gate runs
	uuidBefore := blkid(t, loop, "UUID")

	gate := safety.Gate{Sys: sys, Discover: device.Discover, ShowLoop: true}
	r, ok := gate.FinalGate(loop, confirmed, fp)
	if ok {
		t.Fatalf("FinalGate must fail after the device changed: %+v", r.Findings)
	}
	if !r.Has("CHANGED_UNDER_US") && !r.Has("MOUNTED") {
		t.Fatalf("expected CHANGED_UNDER_US: %+v", r.Findings)
	}

	// executor.Run with that gate emits Gate != nil and spawns nothing:
	// the device is untouched (same UUID).
	argv, _, err := cmdgen.Build(s, map[string]any{"label": "nope"}, nil, loop, false, false)
	if err != nil {
		t.Fatal(err)
	}
	gateFn := func() (safety.Report, bool) { return gate.FinalGate(loop, confirmed, fp) }
	var events []executor.Event
	for ev := range executor.Run(context.Background(), argv, gateFn) {
		events = append(events, ev)
	}
	if len(events) != 1 || events[0].Gate == nil {
		t.Fatalf("expected a single gate event: %+v", events)
	}
	if got := blkid(t, loop, "UUID"); got != uuidBefore {
		t.Fatalf("device was touched despite gate failure: UUID %q -> %q", uuidBefore, got)
	}
	if got := blkid(t, loop, "LABEL"); got == "nope" {
		t.Fatal("format ran despite gate failure")
	}
}

func TestAbort(t *testing.T) {
	requireBinary(t, "mkfs.ext4")
	loop := makeLoop(t, "8G")
	s := schemaByID(t, "ext4")
	argv, _, err := cmdgen.Build(s, map[string]any{}, nil, loop, false, false)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := executor.Run(ctx, argv, func() (safety.Report, bool) { return safety.Report{}, true })
	cancel() // abort immediately

	var done executor.Event
	timeout := time.After(60 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto out
			}
			if ev.Done {
				done = ev
			}
		case <-timeout:
			t.Fatal("executor did not finish after abort")
		}
	}
out:
	if !done.Aborted {
		t.Fatalf("expected Aborted, got %+v", done)
	}
	// The process group is gone.
	if out, err := exec.Command("pgrep", "-f", "mkfs.ext4.*"+loop).Output(); err == nil && len(out) > 0 {
		t.Fatalf("mkfs still running after abort: %s", out)
	}
}

// TestShellQuoteDifferential: the display quoting round-trips through a real
// POSIX shell (spec §13.3 note).
func TestShellQuoteDifferential(t *testing.T) {
	requireBinary(t, "sh")
	cases := []string{
		"plain", "my disk", "it's", "a;b|c&d", "$HOME `date`", `back\slash`,
		"^has_journal", "two  spaces", "*glob?", `"quoted"`, "dash-leading -n",
	}
	for _, want := range cases {
		q := cmdgen.ShellQuote(want)
		out, err := exec.Command("sh", "-c", "printf '%s' "+q).Output()
		if err != nil {
			t.Errorf("sh choked on %q (%s): %v", want, q, err)
			continue
		}
		if string(out) != want {
			t.Errorf("round-trip %q -> %s -> %q", want, q, out)
		}
	}
}
