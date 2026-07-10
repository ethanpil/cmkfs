package device

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func find(t *testing.T, devs []Device, path string) Device {
	t.Helper()
	for _, d := range devs {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("device %s not in %v", path, paths(devs))
	return Device{}
}

func paths(devs []Device) []string {
	out := make([]string, len(devs))
	for i, d := range devs {
		out[i] = d.Path
	}
	return out
}

func TestParsePlainDisk(t *testing.T) {
	devs, err := Parse(fixture(t, "lsblk_plain_disk.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("want 2 devices, got %v", paths(devs))
	}
	disk := find(t, devs, "/dev/sdb")
	if disk.Type != "disk" || disk.SizeBytes != 500107862016 || disk.PTType != "gpt" {
		t.Errorf("disk parsed wrong: %+v", disk)
	}
	if disk.Model != "Samsung SSD 870" || disk.Transport != "sata" || disk.Rotational {
		t.Errorf("disk attrs wrong: %+v", disk)
	}
	if len(disk.Children) != 1 || disk.Children[0] != "/dev/sdb1" {
		t.Errorf("disk children wrong: %v", disk.Children)
	}
	part := find(t, devs, "/dev/sdb1")
	if part.Parent != "/dev/sdb" || part.KName != "sdb1" || part.MajMin != "8:17" {
		t.Errorf("partition tree wiring wrong: %+v", part)
	}
	if part.FSType != "ext4" || part.Label != "data" {
		t.Errorf("partition signature wrong: %+v", part)
	}
	if len(part.Mountpoints) != 1 || part.Mountpoints[0] != "/mnt/data" {
		t.Errorf("mountpoints wrong: %v", part.Mountpoints)
	}
	if part.Model != "Samsung SSD 870" {
		t.Errorf("partition should inherit parent model, got %q", part.Model)
	}
}

func TestParseNvmeParts(t *testing.T) {
	devs, err := Parse(fixture(t, "lsblk_nvme_parts.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 3 {
		t.Fatalf("want 3 devices, got %v", paths(devs))
	}
	root := find(t, devs, "/dev/nvme0n1p2")
	if root.FSType != "ext4" || root.Mountpoints[0] != "/" {
		t.Errorf("root partition wrong: %+v", root)
	}
	if root.Transport != "" { // tran is null on partitions
		t.Errorf("unexpected transport %q", root.Transport)
	}
	disk := find(t, devs, "/dev/nvme0n1")
	if disk.Transport != "nvme" || len(disk.Children) != 2 {
		t.Errorf("nvme disk wrong: %+v", disk)
	}
}

func TestParseLVMStack(t *testing.T) {
	devs, err := Parse(fixture(t, "lsblk_lvm_stack.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 4 {
		t.Fatalf("want 4 devices, got %v", paths(devs))
	}
	lv := find(t, devs, "/dev/mapper/vg0-media")
	if lv.Type != "lvm" || lv.KName != "dm-0" || lv.Parent != "/dev/sdc1" {
		t.Errorf("LV wrong: %+v", lv)
	}
	pv := find(t, devs, "/dev/sdc1")
	if len(pv.Children) != 2 {
		t.Errorf("PV children wrong: %v", pv.Children)
	}
	if pv.FSType != "LVM2_member" {
		t.Errorf("PV fstype wrong: %q", pv.FSType)
	}
	disk := find(t, devs, "/dev/sdc")
	if !disk.Rotational {
		t.Error("HDD should be rotational")
	}
}

func TestParseLoopRoRomZram(t *testing.T) {
	// Without --show-loop: loop excluded; rom and zram always excluded.
	devs, err := Parse(fixture(t, "lsblk_loop_ro.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 || devs[0].Path != "/dev/sdd" {
		t.Fatalf("want only /dev/sdd, got %v", paths(devs))
	}
	usb := devs[0]
	if !usb.ReadOnly || !usb.Removable || usb.Transport != "usb" {
		t.Errorf("RO/removable flags wrong: %+v", usb)
	}
	if usb.FSType != "iso9660" {
		t.Errorf("signature wrong: %+v", usb)
	}

	// With --show-loop: loop included; rom and zram still excluded.
	devs, err = Parse(fixture(t, "lsblk_loop_ro.json"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("want loop + usb, got %v", paths(devs))
	}
	loop := find(t, devs, "/dev/loop9")
	if loop.Type != "loop" || loop.SizeBytes != 2147483648 {
		t.Errorf("loop wrong: %+v", loop)
	}
}

func TestParseLegacyStringFields(t *testing.T) {
	devs, err := Parse(fixture(t, "lsblk_legacy_strings.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	d := find(t, devs, "/dev/sda")
	if d.SizeBytes != 500107862016 || !d.Rotational || d.ReadOnly || d.Removable {
		t.Errorf("legacy string fields parsed wrong: %+v", d)
	}
}

// TestParseExcludedIntermediateKeepsLinkage: a partition below an excluded
// mpath map must remain a child of the nearest included ancestor (the disk),
// or whole-disk safety checks would skip it.
func TestParseExcludedIntermediateKeepsLinkage(t *testing.T) {
	devs, err := Parse(fixture(t, "lsblk_mpath_intermediate.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("want disk + partition (mpath excluded), got %v", paths(devs))
	}
	disk := find(t, devs, "/dev/sdx")
	if len(disk.Children) != 1 || disk.Children[0] != "/dev/mapper/mpatha-part1" {
		t.Fatalf("partition below excluded mpath must be promoted to the disk's children, got %v", disk.Children)
	}
	part := find(t, devs, "/dev/mapper/mpatha-part1")
	if part.Parent != "/dev/sdx" {
		t.Fatalf("promoted partition parent = %q, want /dev/sdx", part.Parent)
	}
}

func TestHumanSizeClamp(t *testing.T) {
	cases := []struct {
		in           int64
		long, short_ string
	}{
		{512, "512 B", "512B"},
		{314572800, "300 MiB", "300M"},
		{1610612736, "1.5 GiB", "1.5G"},
		{1 << 60, "1 EiB", "1.0E"},             // must not panic (suffix clamp)
		{9223372036854775807, "8 EiB", "8.0E"}, // int64 max
	}
	for _, tc := range cases {
		if got := HumanSize(tc.in); got != tc.long {
			t.Errorf("HumanSize(%d) = %q, want %q", tc.in, got, tc.long)
		}
		if got := HumanSizeCompact(tc.in); got != tc.short_ {
			t.Errorf("HumanSizeCompact(%d) = %q, want %q", tc.in, got, tc.short_)
		}
	}
}

func TestParseGarbage(t *testing.T) {
	if _, err := Parse([]byte("not json"), false); err == nil {
		t.Fatal("expected error for garbage input")
	}
}

func TestParseVersionBanner(t *testing.T) {
	cases := []struct {
		binary, banner, want string
	}{
		{"mkfs.ext4", "mke2fs 1.47.0 (5-Feb-2023)\nUsing EXT2FS Library version 1.47.0", "1.47.0"},
		{"mkfs.xfs", "mkfs.xfs version 6.1.1", "6.1.1"},
		{"mkfs.btrfs", "mkfs.btrfs, part of btrfs-progs v6.3", "6.3"},
		{"mkfs.btrfs", "mkfs.btrfs, part of btrfs-progs v6.6.3", "6.6.3"},
		{"mkfs.fat", "mkfs.fat 4.2 (2021-01-31)\nNo device specified.\nUsage: mkfs.fat [OPTIONS] TARGET [BLOCKS]", "4.2"},
		{"mkfs.exfat", "exfatprogs version : 1.2.2", "1.2.2"},
		{"mkfs.exfat", "mkexfatfs 1.3.0", ""}, // legacy fuse exfat-utils: unparseable by design
		{"mkfs.f2fs", "\n\tF2FS-tools: mkfs.f2fs Ver: 1.15.0 (2022-05-13)\n", "1.15.0"},
		{"mkfs.ext4", "some garbage banner", ""},
		{"mkfs.xfs", "", ""},
		{"mkfs.unknown", "whatever 1.0", ""},
	}
	for _, tc := range cases {
		if got := ParseVersionBanner(tc.binary, tc.banner); got != tc.want {
			t.Errorf("ParseVersionBanner(%s, %q) = %q, want %q", tc.binary, tc.banner, got, tc.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.43", "1.43", 0},
		{"1.42", "1.43", -1},
		{"1.47.0", "1.43", 1},
		{"4.5.0", "5.0.0", -1},
		{"5.0", "5.0.0", 0},
		{"6.3", "5.5", 1},
		{"5.10", "5.9", 1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
