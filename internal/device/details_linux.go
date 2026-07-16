//go:build linux

package device

import (
	"strings"
	"syscall"
)

// CollectDetails gathers the info-screen extras: statfs on each mountpoint
// and a few sysfs reads. Missing sources leave zero values.
//
// Not free, despite reading like plain file access. statfs blocks until the
// filesystem answers — indefinitely on a wedged mount — and the hwmon
// temperature is a live device command: the kernel issues an ATA SCT/SMART
// read (NVMe: an admin Get-Log-Page), which will spin up a standby drive.
// Callers must keep this off the UI event loop.
func CollectDetails(d Device) Details {
	det := Details{Mounts: mountUsage(d.Mountpoints)}
	collectSysfs(&det, "/sys", d.KName)
	return det
}

func mountUsage(mounts []string) []MountUsage {
	var out []MountUsage
	for _, m := range mounts {
		if strings.HasPrefix(m, "[") {
			continue // pseudo-mounts like [SWAP] have no filesystem to stat
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(m, &st); err != nil {
			continue
		}
		bs := int64(st.Bsize)
		out = append(out, MountUsage{
			Mountpoint: m,
			TotalBytes: int64(st.Blocks) * bs,
			FreeBytes:  int64(st.Bavail) * bs,
		})
	}
	return out
}
