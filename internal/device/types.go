// Package device enumerates block devices via lsblk --json and probes the
// mkfs backends (spec §8).
package device

// Device is one block device from lsblk.
type Device struct {
	Path        string // /dev/sdb1 (lsblk NAME with --paths)
	KName       string // kernel name, e.g. sdb1 or dm-0 (basename of KNAME)
	MajMin      string // "8:17"
	Type        string // disk | part | lvm | crypt | loop | raid...
	SizeBytes   int64
	FSType      string // existing signature per lsblk ("" if none)
	Label       string
	UUID        string
	PTType      string   // partition table type ("" if none)
	Mountpoints []string // all mountpoints incl. "[SWAP]"
	Model       string   // from parent disk for partitions
	Serial      string
	Transport   string // sata, nvme, usb...
	Rotational  bool
	ReadOnly    bool
	Removable   bool
	Parent      string   // path of parent device, "" for whole disks
	Children    []string // paths of children: partitions of a disk, holders below
}
