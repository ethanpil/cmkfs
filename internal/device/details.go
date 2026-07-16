package device

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MountUsage is statfs-derived usage for one mountpoint.
type MountUsage struct {
	Mountpoint string
	TotalBytes int64
	FreeBytes  int64
}

// Details holds extra per-device information gathered on demand for the
// info screen. Every field is best-effort: zero values mean "unknown".
type Details struct {
	Mounts            []MountUsage
	LogicalBlockSize  int64
	PhysicalBlockSize int64
	TempCelsius       float64
	HasTemp           bool
}

// collectSysfs fills the sysfs-derived fields for a device by kernel name.
// It is plain file access with no Linux-only syscall, so it builds and tests
// on every platform even though only Linux has a real /sys to point it at.
func collectSysfs(det *Details, sysRoot, kname string) {
	// Queue limits and the hwmon live on the whole disk, so a partition has
	// to read its parent's node. /sys/class/block/<name> is a symlink into
	// the device tree, and ".." is only meaningful once no symlinks are left
	// in the path: filepath.Join cleans "<name>/.." away lexically, which
	// addresses a sibling of /sys/class/block that never exists. Resolve the
	// link first, then the parent directory is an ordinary lexical step.
	dirs := []string{filepath.Join(sysRoot, "class", "block", kname)}
	if real, err := filepath.EvalSymlinks(dirs[0]); err == nil {
		dirs[0] = real
		dirs = append(dirs, filepath.Dir(real))
	}

	for _, dir := range dirs {
		if v, ok := readSysInt(filepath.Join(dir, "queue", "logical_block_size")); ok {
			det.LogicalBlockSize = v
			if p, ok := readSysInt(filepath.Join(dir, "queue", "physical_block_size")); ok {
				det.PhysicalBlockSize = p
			}
			break
		}
	}

	// Drive temperature when a hwmon is bound (the drivetemp module for
	// SATA/SAS, the nvme core for NVMe). temp1_input is milli-°C.
	for _, dir := range dirs {
		for _, pat := range []string{
			filepath.Join(dir, "device", "hwmon", "hwmon*", "temp1_input"), // SATA drivetemp
			filepath.Join(dir, "device", "hwmon*", "temp1_input"),          // NVMe controller
		} {
			matches, _ := filepath.Glob(pat)
			for _, m := range matches {
				if v, ok := readSysInt(m); ok {
					det.TempCelsius = float64(v) / 1000
					det.HasTemp = true
					return
				}
			}
		}
	}
}

func readSysInt(path string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
