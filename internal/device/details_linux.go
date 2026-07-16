//go:build linux

package device

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CollectDetails gathers the info-screen extras: statfs on each mountpoint
// and a few sysfs reads. Everything here is read-only and near-zero cost
// (no exec, no ioctl, no device I/O); missing sources leave zero values.
func CollectDetails(d Device) Details {
	var det Details
	for _, m := range d.Mountpoints {
		if strings.HasPrefix(m, "[") {
			continue // pseudo-mounts like [SWAP] have no filesystem to stat
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(m, &st); err == nil {
			bs := int64(st.Bsize)
			det.Mounts = append(det.Mounts, MountUsage{
				Mountpoint: m,
				TotalBytes: int64(st.Blocks) * bs,
				FreeBytes:  int64(st.Bavail) * bs,
			})
		}
	}

	// Queue limits live on the whole disk. /sys/class/block/<part> resolves
	// through its symlink into the parent disk's directory, so ".." reaches
	// the disk's sysfs node for partitions.
	sys := "/sys/class/block/" + d.KName
	for _, dir := range []string{sys, sys + "/.."} {
		if v, ok := readSysInt(filepath.Join(dir, "queue", "logical_block_size")); ok {
			det.LogicalBlockSize = v
			det.PhysicalBlockSize, _ = readSysInt(filepath.Join(dir, "queue", "physical_block_size"))
			break
		}
	}

	// Drive temperature when a hwmon is bound (the drivetemp module for
	// SATA/SAS, the nvme core for NVMe). temp1_input is milli-°C.
	for _, pat := range []string{
		sys + "/device/hwmon/hwmon*/temp1_input", // SATA drivetemp
		sys + "/device/hwmon*/temp1_input",       // NVMe controller
		sys + "/../device/hwmon/hwmon*/temp1_input",
		sys + "/../device/hwmon*/temp1_input",
	} {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if v, ok := readSysInt(m); ok {
				det.TempCelsius = float64(v) / 1000
				det.HasTemp = true
				break
			}
		}
		if det.HasTemp {
			break
		}
	}
	return det
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
