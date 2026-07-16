package device

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSysfs builds the shape the real thing has: the block nodes live in the
// device tree, /sys/class/block holds symlinks to them, and only the whole
// disk carries queue/ and device/. The symlink is the point — it is what
// makes a lexical ".." address the wrong directory.
//
//	<root>/devices/pci0000_00/block/sda/{queue,device/hwmon/hwmon3}
//	<root>/devices/pci0000_00/block/sda/sda1/          (partition: bare)
//	<root>/class/block/{sda,sda1} -> ../../devices/...
func fakeSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	disk := filepath.Join(root, "devices", "pci0000_00", "block", "sda")
	part := filepath.Join(disk, "sda1")
	hwmon := filepath.Join(disk, "device", "hwmon", "hwmon3")

	for _, d := range []string{filepath.Join(disk, "queue"), part, hwmon, filepath.Join(root, "class", "block")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, val string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(val), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(disk, "queue", "logical_block_size"), "512\n")
	write(filepath.Join(disk, "queue", "physical_block_size"), "4096\n")
	write(filepath.Join(hwmon, "temp1_input"), "43500\n")

	for name, target := range map[string]string{"sda": disk, "sda1": part} {
		if err := os.Symlink(target, filepath.Join(root, "class", "block", name)); err != nil {
			// Unprivileged symlinks need Developer Mode on Windows; Linux CI
			// is the authoritative gate and always has them.
			t.Skipf("symlinks unavailable on this host: %v", err)
		}
	}
	return root
}

// A partition has no queue/ or device/ of its own, so both the block size and
// the temperature have to come from the parent disk. Reaching it means
// resolving the /sys/class/block symlink first: appending ".." to the link
// path and letting filepath.Join clean it silently addresses
// <root>/class/block/queue, which never exists.
func TestCollectSysfsPartitionReadsParentDisk(t *testing.T) {
	var det Details
	collectSysfs(&det, fakeSysfs(t), "sda1")

	if det.LogicalBlockSize != 512 {
		t.Errorf("LogicalBlockSize = %d, want 512 (from the parent disk)", det.LogicalBlockSize)
	}
	if det.PhysicalBlockSize != 4096 {
		t.Errorf("PhysicalBlockSize = %d, want 4096 (from the parent disk)", det.PhysicalBlockSize)
	}
	if !det.HasTemp || det.TempCelsius != 43.5 {
		t.Errorf("temp = %v/%v, want true/43.5 (from the parent disk's hwmon)", det.HasTemp, det.TempCelsius)
	}
}

func TestCollectSysfsWholeDisk(t *testing.T) {
	var det Details
	collectSysfs(&det, fakeSysfs(t), "sda")

	if det.LogicalBlockSize != 512 || det.PhysicalBlockSize != 4096 {
		t.Errorf("block size = %d/%d, want 512/4096", det.LogicalBlockSize, det.PhysicalBlockSize)
	}
	if !det.HasTemp || det.TempCelsius != 43.5 {
		t.Errorf("temp = %v/%v, want true/43.5", det.HasTemp, det.TempCelsius)
	}
}

// Every source is best-effort: an unknown device must yield zero values
// rather than an error or a panic.
func TestCollectSysfsMissingSources(t *testing.T) {
	var det Details
	collectSysfs(&det, fakeSysfs(t), "nope")
	if det.LogicalBlockSize != 0 || det.PhysicalBlockSize != 0 || det.HasTemp {
		t.Errorf("unknown device must leave zero values, got %+v", det)
	}
}

// physical_block_size missing while logical is readable must leave physical
// at 0 (unknown) so the view can omit it rather than claim 0 B sectors.
func TestCollectSysfsPhysicalMissing(t *testing.T) {
	root := fakeSysfs(t)
	if err := os.Remove(filepath.Join(root, "devices", "pci0000_00", "block", "sda", "queue", "physical_block_size")); err != nil {
		t.Fatal(err)
	}
	var det Details
	collectSysfs(&det, root, "sda")
	if det.LogicalBlockSize != 512 {
		t.Errorf("LogicalBlockSize = %d, want 512", det.LogicalBlockSize)
	}
	if det.PhysicalBlockSize != 0 {
		t.Errorf("PhysicalBlockSize = %d, want 0 (unknown)", det.PhysicalBlockSize)
	}
}
