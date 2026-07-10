package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethanpil/cmkfs/internal/device"
)

// testDevices models the machine described by testdata/basic:
//
//	nvme0n1 (system disk: p1 = /boot/efi, p2 = /)
//	sdb     (sdb1 mounted at /mnt/data, sdb2 active swap)
//	sdc     (multipath path device: holder dm-0 in sysfs; child sdc1)
//	sdd     (read-only per sysfs)
//	sde     (clean bare disk)
//	sdf     (gpt disk, sdf1 = xfs signature 'backup', mounted with space in path)
//	sdg     (clean USB stick, removable)
func testDevices() []device.Device {
	return []device.Device{
		{Path: "/dev/nvme0n1", KName: "nvme0n1", MajMin: "259:0", Type: "disk", SizeBytes: 1024209543168, PTType: "gpt", Children: []string{"/dev/nvme0n1p1", "/dev/nvme0n1p2"}},
		{Path: "/dev/nvme0n1p1", KName: "nvme0n1p1", MajMin: "259:1", Type: "part", SizeBytes: 536870912, FSType: "vfat", Parent: "/dev/nvme0n1"},
		{Path: "/dev/nvme0n1p2", KName: "nvme0n1p2", MajMin: "259:2", Type: "part", SizeBytes: 1023671623680, FSType: "ext4", Parent: "/dev/nvme0n1"},
		{Path: "/dev/sdb", KName: "sdb", MajMin: "8:16", Type: "disk", SizeBytes: 500107862016, PTType: "gpt", Children: []string{"/dev/sdb1", "/dev/sdb2"}},
		{Path: "/dev/sdb1", KName: "sdb1", MajMin: "8:17", Type: "part", SizeBytes: 491045163008, FSType: "ext4", Parent: "/dev/sdb"},
		{Path: "/dev/sdb2", KName: "sdb2", MajMin: "8:18", Type: "part", SizeBytes: 8589934592, FSType: "swap", Parent: "/dev/sdb"},
		{Path: "/dev/sdc", KName: "sdc", MajMin: "8:32", Type: "disk", SizeBytes: 2000398934016, PTType: "dos", Children: []string{"/dev/sdc1"}},
		{Path: "/dev/sdc1", KName: "sdc1", MajMin: "8:33", Type: "part", SizeBytes: 2000397795328, Parent: "/dev/sdc"},
		{Path: "/dev/sdd", KName: "sdd", MajMin: "8:48", Type: "disk", SizeBytes: 31268536320},
		{Path: "/dev/sde", KName: "sde", MajMin: "8:64", Type: "disk", SizeBytes: 500107862016},
		{Path: "/dev/sdf", KName: "sdf", MajMin: "8:80", Type: "disk", SizeBytes: 4000787030016, PTType: "gpt", Children: []string{"/dev/sdf1"}},
		{Path: "/dev/sdf1", KName: "sdf1", MajMin: "8:81", Type: "part", SizeBytes: 4000785982464, FSType: "xfs", Label: "backup", UUID: "5a4b3c2d-3333-4c7d-9e5f-0123456789ab", Parent: "/dev/sdf"},
		{Path: "/dev/sdg", KName: "sdg", MajMin: "8:96", Type: "disk", SizeBytes: 31268536320, Removable: true, Transport: "usb"},
	}
}

func testSystem(t *testing.T) System {
	t.Helper()
	return System{
		ProcRoot: filepath.Join("testdata", "basic", "proc"),
		SysRoot:  filepath.Join("testdata", "basic", "sys"),
	}
}

func devByPath(t *testing.T, path string) device.Device {
	t.Helper()
	for _, d := range testDevices() {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("no test device %s", path)
	return device.Device{}
}

func check(t *testing.T, path string, mod func(*Params)) Report {
	t.Helper()
	p := Params{Device: devByPath(t, path), All: testDevices()}
	if mod != nil {
		mod(&p)
	}
	return testSystem(t).Check(p)
}

func assertFinding(t *testing.T, r Report, sev Severity, code, msgPart string) {
	t.Helper()
	for _, f := range r.Findings {
		if f.Code == code {
			if f.Severity != sev {
				t.Errorf("%s: severity %v, want %v", code, f.Severity, sev)
			}
			if msgPart != "" && !strings.Contains(f.Message, msgPart) {
				t.Errorf("%s message %q does not contain %q", code, f.Message, msgPart)
			}
			return
		}
	}
	t.Errorf("finding %s not present in %+v", code, r.Findings)
}

func TestMounted(t *testing.T) {
	r := check(t, "/dev/sdb1", nil)
	assertFinding(t, r, Blocker, "MOUNTED", "/dev/sdb1 is mounted at /mnt/data. Unmount it first.")
	if !r.Blocked() {
		t.Error("mounted device must be blocked")
	}
}

func TestMountedWholeDiskViaChild(t *testing.T) {
	// The whole disk sdb is blocked because child sdb1 is mounted.
	r := check(t, "/dev/sdb", nil)
	assertFinding(t, r, Blocker, "MOUNTED", "/dev/sdb1 is mounted at /mnt/data")
}

func TestMountpointWithSpace(t *testing.T) {
	r := check(t, "/dev/sdf1", nil)
	assertFinding(t, r, Blocker, "MOUNTED", "/mnt/my backup")
}

func TestActiveSwap(t *testing.T) {
	r := check(t, "/dev/sdb2", nil)
	assertFinding(t, r, Blocker, "ACTIVE_SWAP", "/dev/sdb2 is active swap. Run swapoff first.")
	// And transitively for the parent disk.
	r = check(t, "/dev/sdb", nil)
	assertFinding(t, r, Blocker, "ACTIVE_SWAP", "swapoff")
}

// TestActiveSwapAlias: swap enabled under an alias path (by-id/by-uuid) must
// still be detected via kernel-name matching.
func TestActiveSwapAlias(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	swaps := "Filename\t\tType\t\tSize\t\tUsed\t\tPriority\n" +
		"/dev/some/alias/sdb2 partition\t8388604\t0\t-2\n"
	if err := os.WriteFile(filepath.Join(dir, "swaps"), []byte(swaps), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "self", "mountinfo"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sys := System{ProcRoot: dir, SysRoot: t.TempDir()}
	r := sys.Check(Params{Device: devByPath(t, "/dev/sdb2"), All: testDevices()})
	assertFinding(t, r, Blocker, "ACTIVE_SWAP", "swapoff")
}

// TestSignaturePTableOnLoop: a whole non-disk device (loop image) carrying a
// partition table must warn and force-inject like a disk would.
func TestSignaturePTableOnLoop(t *testing.T) {
	loop := device.Device{Path: "/dev/loop7", KName: "loop7", MajMin: "7:7", Type: "loop", SizeBytes: 2147483648, PTType: "gpt"}
	r := testSystem(t).Check(Params{Device: loop, All: []device.Device{loop}})
	assertFinding(t, r, Warning, "SIGNATURE_PTABLE", "Existing gpt partition table will be destroyed.")
	if !r.NeedsForce() {
		t.Error("partition table on a loop device must trigger force injection")
	}
}

// TestSignaturePTableNotOnPartition: a partition's PTType reflects the table
// it belongs to, not one inside it — no warning.
func TestSignaturePTableNotOnPartition(t *testing.T) {
	part := devByPath(t, "/dev/sdc1")
	part.PTType = "dos" // as lsblk reports for members of a dos-labelled disk
	r := testSystem(t).Check(Params{Device: part, All: testDevices()})
	if r.Has("SIGNATURE_PTABLE") {
		t.Error("SIGNATURE_PTABLE must not fire for a partition")
	}
}

func TestReadOnlyFromLsblk(t *testing.T) {
	p := devByPath(t, "/dev/sde")
	p.ReadOnly = true
	r := testSystem(t).Check(Params{Device: p, All: testDevices()})
	assertFinding(t, r, Blocker, "READ_ONLY", "read-only")
}

func TestReadOnlyFromSysfsReread(t *testing.T) {
	// sdd has RO=0 in the device struct but sysfs says ro=1 (the kernel
	// flipped it after enumeration).
	r := check(t, "/dev/sdd", nil)
	assertFinding(t, r, Blocker, "READ_ONLY", "/dev/sdd is read-only.")
}

func TestInUseHolderDirect(t *testing.T) {
	r := check(t, "/dev/sdc", nil)
	assertFinding(t, r, Blocker, "IN_USE_HOLDER", "/dev/sdc is in use by dm-0")
}

func TestInUseHolderTransitiveMultipath(t *testing.T) {
	// sdc1 is a partition of the multipath path device sdc; the dm map
	// appears as sdc's holder, and transitivity must catch it.
	r := check(t, "/dev/sdc1", nil)
	assertFinding(t, r, Blocker, "IN_USE_HOLDER", "in use by dm-0")
}

func TestDeviceBusyProbe(t *testing.T) {
	sys := testSystem(t)
	sys.ProbeExcl = func(path string) error { return ErrBusy }
	r := sys.Check(Params{Device: devByPath(t, "/dev/sde"), All: testDevices(), Probe: true})
	assertFinding(t, r, Blocker, "DEVICE_BUSY", "/dev/sde is exclusively held by another process.")

	// Probe not requested: no DEVICE_BUSY finding even with a busy probe.
	r = sys.Check(Params{Device: devByPath(t, "/dev/sde"), All: testDevices()})
	if r.Has("DEVICE_BUSY") {
		t.Error("probe must not run when Params.Probe is false")
	}

	// Probe succeeding: no finding.
	sys.ProbeExcl = func(path string) error { return nil }
	r = sys.Check(Params{Device: devByPath(t, "/dev/sde"), All: testDevices(), Probe: true})
	if r.Has("DEVICE_BUSY") {
		t.Error("free device must not be DEVICE_BUSY")
	}
}

func TestExtraArgsWarning(t *testing.T) {
	r := check(t, "/dev/sde", func(p *Params) { p.ExtraArgs = 2 })
	assertFinding(t, r, Warning, "EXTRA_ARGS", "2 unsupported extra arguments will be passed to mkfs unvalidated.")
	if r.NeedsForce() {
		t.Error("EXTRA_ARGS must never trigger force injection")
	}
	if !r.HasWarnings() {
		t.Error("EXTRA_ARGS must force typed confirmation")
	}
}

func TestSystemDiskRootPartition(t *testing.T) {
	r := check(t, "/dev/nvme0n1p2", nil)
	assertFinding(t, r, Blocker, "SYSTEM_DISK", "contains the running system's root filesystem")
}

func TestSystemDiskAncestor(t *testing.T) {
	// The whole nvme disk is an ancestor of the root partition.
	r := check(t, "/dev/nvme0n1", nil)
	assertFinding(t, r, Blocker, "SYSTEM_DISK", "root filesystem")
}

func TestSystemDiskSibling(t *testing.T) {
	// The EFI partition backs /boot/efi and is itself protected.
	r := check(t, "/dev/nvme0n1p1", nil)
	assertFinding(t, r, Blocker, "SYSTEM_DISK", "root filesystem")
}

func TestWholeDisk(t *testing.T) {
	r := check(t, "/dev/sdb", nil)
	assertFinding(t, r, Warning, "WHOLE_DISK", "/dev/sdb is an entire disk containing 2 partitions. All of them will be destroyed.")
}

func TestWholeDiskBare(t *testing.T) {
	r := check(t, "/dev/sde", nil)
	assertFinding(t, r, Info, "WHOLE_DISK_BARE", "Formatting the whole disk without a partition table.")
	if r.Blocked() || r.HasWarnings() {
		t.Errorf("bare disk must have no blockers or warnings: %+v", r.Findings)
	}
}

func TestSignatureFS(t *testing.T) {
	// sdf1 is mounted in the fixture; signature finding must still appear.
	r := check(t, "/dev/sdf1", nil)
	assertFinding(t, r, Warning, "SIGNATURE_FS", "Existing xfs filesystem (label 'backup', UUID 5a4b3c2d-3333-4c7d-9e5f-0123456789ab) will be destroyed.")
	if !r.NeedsForce() {
		t.Error("SIGNATURE_FS must trigger force injection")
	}
}

func TestSignaturePTable(t *testing.T) {
	r := check(t, "/dev/sdb", nil)
	assertFinding(t, r, Warning, "SIGNATURE_PTABLE", "Existing gpt partition table will be destroyed.")
	if !r.NeedsForce() {
		t.Error("SIGNATURE_PTABLE must trigger force injection")
	}
}

func TestTooSmall(t *testing.T) {
	r := check(t, "/dev/sdg", func(p *Params) {
		p.MinSizeBytes = 314572800
		p.FSName = "XFS"
		p.Device.SizeBytes = 134217728 // 128 MiB
	})
	assertFinding(t, r, Blocker, "TOO_SMALL", "XFS requires at least 300 MiB")
	if !r.Has("TOO_SMALL") {
		t.Fatal("expected TOO_SMALL")
	}
}

func TestRemovable(t *testing.T) {
	r := check(t, "/dev/sdg", nil)
	assertFinding(t, r, Info, "REMOVABLE", "Removable device (USB). Double-check it's the right one.")
	if r.Blocked() {
		t.Error("removable alone must not block")
	}
}

func TestCleanDeviceNeedsNoForce(t *testing.T) {
	r := check(t, "/dev/sde", nil)
	if r.NeedsForce() {
		t.Errorf("clean device must not need force: %+v", r.Findings)
	}
	if r.Blocked() {
		t.Errorf("clean device must not be blocked: %+v", r.Findings)
	}
}

func TestParseMountinfoMalformed(t *testing.T) {
	entries := parseMountinfo("garbage\n\n1 2\n36 22 8:17 / /mnt rw - ext4 /dev/sdb1 rw\n")
	if len(entries) != 1 || entries[0].MajMin != "8:17" || entries[0].Source != "/dev/sdb1" {
		t.Errorf("unexpected parse: %+v", entries)
	}
}

func TestFingerprintOf(t *testing.T) {
	d := devByPath(t, "/dev/sdf1")
	fp := FingerprintOf(d)
	want := Fingerprint{SizeBytes: 4000785982464, FSType: "xfs", FSUUID: "5a4b3c2d-3333-4c7d-9e5f-0123456789ab", PTType: ""}
	if fp != want {
		t.Errorf("fingerprint %+v, want %+v", fp, want)
	}
}

// --- FinalGate ---

func testGate(t *testing.T, devs []device.Device, discoverErr error) Gate {
	t.Helper()
	return Gate{
		Sys: testSystem(t),
		Discover: func(showLoop bool) ([]device.Device, error) {
			return devs, discoverErr
		},
	}
}

func TestFinalGateOK(t *testing.T) {
	devs := testDevices()
	target := devByPath(t, "/dev/sde")
	confirmed := testSystem(t).Check(Params{Device: target, All: devs})
	fp := FingerprintOf(target)
	r, ok := testGate(t, devs, nil).FinalGate("/dev/sde", confirmed, fp)
	if !ok {
		t.Fatalf("gate must pass for unchanged clean device: %+v", r.Findings)
	}
}

func TestFinalGateFingerprintMismatch(t *testing.T) {
	devs := testDevices()
	target := devByPath(t, "/dev/sde")
	confirmed := testSystem(t).Check(Params{Device: target, All: devs})
	fp := FingerprintOf(target)

	// Someone re-formatted the device while the confirm screen sat open.
	for i := range devs {
		if devs[i].Path == "/dev/sde" {
			devs[i].FSType = "xfs"
			devs[i].UUID = "11111111-2222-3333-4444-555555555555"
		}
	}
	r, ok := testGate(t, devs, nil).FinalGate("/dev/sde", confirmed, fp)
	if ok {
		t.Fatal("gate must fail on fingerprint mismatch")
	}
	assertFinding(t, r, Blocker, "CHANGED_UNDER_US", "was none, now xfs")
}

func TestFinalGateNewWarning(t *testing.T) {
	devs := testDevices()
	target := devByPath(t, "/dev/sde")
	// Confirmed against a clean report...
	confirmed := testSystem(t).Check(Params{Device: target, All: devs})
	// ...but the fingerprint was captured against the changed state, so only
	// the warning is new (no CHANGED_UNDER_US masking this path).
	for i := range devs {
		if devs[i].Path == "/dev/sde" {
			devs[i].FSType = "ext4"
		}
	}
	fp := FingerprintOf(devs[9])
	r, ok := testGate(t, devs, nil).FinalGate("/dev/sde", confirmed, fp)
	if ok {
		t.Fatalf("gate must fail on a Warning absent from the confirmed report: %+v", r.Findings)
	}
	if r.Blocked() {
		t.Fatalf("this path must fail via new-warning, not blocker: %+v", r.Findings)
	}
}

func TestFinalGateNewBlocker(t *testing.T) {
	devs := testDevices()
	target := devByPath(t, "/dev/sdb1") // mounted in fixture
	confirmed := Report{}               // pretend it was clean at confirm time
	fp := FingerprintOf(target)
	r, ok := testGate(t, devs, nil).FinalGate("/dev/sdb1", confirmed, fp)
	if ok {
		t.Fatal("gate must fail when the device was mounted meanwhile")
	}
	assertFinding(t, r, Blocker, "MOUNTED", "/dev/sdb1 is mounted")
}

func TestFinalGateDeviceDisappeared(t *testing.T) {
	r, ok := testGate(t, nil, nil).FinalGate("/dev/sde", Report{}, Fingerprint{})
	if ok {
		t.Fatal("gate must fail when the device disappeared")
	}
	assertFinding(t, r, Blocker, "CHANGED_UNDER_US", "disappeared")
}

func TestFinalGateDiscoverError(t *testing.T) {
	r, ok := testGate(t, nil, errors.New("lsblk exploded")).FinalGate("/dev/sde", Report{}, Fingerprint{})
	if ok {
		t.Fatal("gate must fail when re-enumeration fails")
	}
	assertFinding(t, r, Blocker, "CHANGED_UNDER_US", "cannot re-enumerate")
}

func TestFinalGateBusyProbe(t *testing.T) {
	devs := testDevices()
	target := devByPath(t, "/dev/sde")
	confirmed := testSystem(t).Check(Params{Device: target, All: devs})
	fp := FingerprintOf(target)
	g := testGate(t, devs, nil)
	g.Sys.ProbeExcl = func(path string) error {
		if path != "/dev/sde" {
			return fmt.Errorf("probed wrong device %s", path)
		}
		return ErrBusy
	}
	r, ok := g.FinalGate("/dev/sde", confirmed, fp)
	if ok {
		t.Fatal("gate must fail when the O_EXCL probe reports busy")
	}
	assertFinding(t, r, Blocker, "DEVICE_BUSY", "exclusively held")
}
