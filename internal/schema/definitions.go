package schema

// Schemas is the complete compiled-in schema set. This file is data-only by
// rule (spec §5.1): composite literals exclusively — no function calls, no
// conditionals, no arithmetic (i64 is the one permitted helper, defined in
// schema.go).

var Schemas = []Schema{ext4, xfs, btrfs, vfat, exfat, f2fs}

var ext4 = Schema{
	ID:          "ext4",
	Name:        "ext4",
	Description: "Fourth extended filesystem. Default on most Linux distributions.",
	Binary:      "mkfs.ext4",
	ForceFlag:   "-F",
	MinVersion:  "1.43",
	Options: []Option{
		{
			ID:          "label",
			Name:        "Volume label",
			Description: "Human-readable name for the filesystem, shown by lsblk and file managers. Max 16 bytes.",
			Type:        KindString,
			Default:     "",
			Flag:        "-L {value}",
			MaxBytes:    16,
			Pattern:     `^[^\x00-\x1f]*$`,
			Placeholder: "(none)",
		},
		{
			ID:          "uuid",
			Name:        "UUID",
			Description: "Set a specific filesystem UUID instead of a random one. Useful when restoring an fstab entry that references a UUID.",
			Type:        KindString,
			Default:     "",
			Flag:        "-U {value}",
			Pattern:     `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			Placeholder: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		},
		{
			ID:          "block_size",
			Name:        "Block size",
			Description: "Filesystem block size in bytes. Leave at backend default (mke2fs picks based on filesystem size) unless you have a specific reason.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-b {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (4096)", Help: "mke2fs chooses based on device size; 4 KiB on anything modern."},
				{Value: "1024", Label: "1 KiB", Help: "Small files on small filesystems."},
				{Value: "2048", Label: "2 KiB"},
				{Value: "4096", Label: "4 KiB", Help: "Standard for virtually all modern disks."},
			},
		},
		{
			ID:          "usage_type",
			Name:        "Usage type",
			Description: "Preset tuning profile from mke2fs.conf. Adjusts inode density (and block size for some types) for the expected file-size mix.",
			LongHelp: `Every file needs one inode, and inodes are allocated at format time — they
permanently consume space and cannot be added later. Usage types are presets
that pick a sensible inode density for what you'll store:

  small       one inode per 4 KiB — millions of tiny files (mail spools).
  largefile   one inode per 1 MiB — mostly large files.
  largefile4  one inode per 4 MiB — very large files only: media libraries,
              VM images, backup archives. Frees noticeable space and makes
              mkfs/fsck much faster, but the filesystem will run out of
              inodes if you later fill it with many small files.
  big / huge  profiles mke2fs would pick automatically for large devices.

Leave on Auto unless you know your file-size mix. To set density by hand
instead, use Bytes per inode (the two are mutually exclusive).`,
			Type:      KindEnum,
			Default:   "auto",
			Omit:      "auto",
			Flag:      "-T {value}",
			Conflicts: []string{"bytes_per_inode", "inode_size"},
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (by device size)", Help: "mke2fs picks small/default/big/huge by device size."},
				{Value: "small", Label: "small — many tiny files", Help: "1 KiB blocks, one inode per 4 KiB."},
				{Value: "largefile", Label: "largefile — 1 MiB/inode", Help: "One inode per 1 MiB. Fewer inodes, faster mkfs/fsck, more usable space."},
				{Value: "largefile4", Label: "largefile4 — 4 MiB/inode", Help: "One inode per 4 MiB. For volumes of very large files (media, backups, VM images)."},
				{Value: "big", Label: "big", Help: "Profile for large filesystems."},
				{Value: "huge", Label: "huge", Help: "Profile for very large filesystems."},
			},
		},
		{
			ID:          "bytes_per_inode",
			Name:        "Bytes per inode",
			Description: "One inode per this many bytes. Larger = fewer inodes, more usable space, but you can run out of inodes. Mutually exclusive with Usage type.",
			Type:        KindInt,
			Default:     int64(0),
			Omit:        int64(0),
			Flag:        "-i {value}",
			Min:         i64(1024),
			Max:         i64(67108864),
			Conflicts:   []string{"usage_type"},
			Placeholder: "16384",
		},
		{
			ID:          "inode_size",
			Name:        "Inode size",
			Description: "On-disk size of each inode in bytes. 256 (the default) is required for full timestamps past 2038 and inline extended attributes. Mutually exclusive with Usage type.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-I {value}",
			Conflicts:   []string{"usage_type"},
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (256)"},
				{Value: "128", Label: "128 bytes", Help: "Legacy. Breaks y2038 timestamps. Avoid."},
				{Value: "256", Label: "256 bytes", Help: "Modern default."},
				{Value: "512", Label: "512 bytes", Help: "Extra room for extended attributes."},
				{Value: "1024", Label: "1024 bytes"},
			},
		},
		{
			ID:          "reserved_percent",
			Name:        "Reserved blocks %",
			Description: "Percentage of blocks reserved for root, so the system stays usable when the disk fills. Backend default is 5. Data-only volumes are commonly set to 0 or 1.",
			Type:        KindInt,
			Default:     int64(-1),
			Omit:        int64(-1),
			Flag:        "-m {value}",
			Min:         i64(0),
			Max:         i64(50),
			Placeholder: "5",
		},
		{
			ID:          "journal",
			Name:        "Journal",
			Description: "Keep the ext4 journal enabled (strongly recommended). Disabling it means slightly faster writes and no crash consistency.",
			Type:        KindBool,
			Default:     true,
			FlagFalse:   "-O ^has_journal",
		},
	},
}

var vfat = Schema{
	ID:            "vfat",
	Name:          "FAT32 (vfat)",
	Description:   "The universally readable FAT filesystem — firmware, cameras, EFI partitions, USB media. No permissions; 4 GiB max file size.",
	Binary:        "mkfs.fat",
	ForceFlag:     "",   // mkfs.fat overwrites signatures unconditionally; see spec §9
	WholeDiskFlag: "-I", // mkfs.fat refuses entire-disk targets without it
	MinVersion:    "4.1",
	Options: []Option{
		{
			ID:          "label",
			Name:        "Volume label",
			Description: "Human-readable name for the filesystem. FAT labels are max 11 bytes: uppercase letters, digits, underscore, dash, and space.",
			Type:        KindString,
			Default:     "",
			Flag:        "-n {value}",
			MaxBytes:    11,
			Pattern:     `^[A-Z0-9_\- ]*$`,
			Placeholder: "(none)",
		},
		{
			ID:          "fat_size",
			Name:        "FAT size",
			Description: "Width of FAT table entries: 12, 16, or 32 bits. Auto lets mkfs.fat pick by device size. FAT32 is required beyond ~2 GiB; some firmware expects a specific size.",
			LongHelp: `The FAT size sets how many clusters the filesystem can address, and with it
the practical volume size range:

  FAT12  up to a few MiB — floppies and tiny flash media.
  FAT16  roughly 16 MiB to 2 GiB (larger with big clusters).
  FAT32  512 MiB up to 2 TiB; the only choice for most modern media.

Auto picks by device size and is right for normal use. Set it explicitly
when a consumer demands a specific FAT width — EFI system partitions are
commonly formatted FAT32 regardless of size, while some embedded devices
and older firmware only read FAT16. mkfs.fat fails if the requested size
cannot address the device with the available cluster sizes.`,
			Type:    KindEnum,
			Default: "auto",
			Omit:    "auto",
			Flag:    "-F {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (by device size)", Help: "mkfs.fat picks 12/16/32 by device size."},
				{Value: "12", Label: "FAT12", Help: "Tiny media only (up to a few MiB)."},
				{Value: "16", Label: "FAT16", Help: "Small media up to ~2 GiB; expected by some embedded firmware."},
				{Value: "32", Label: "FAT32", Help: "Standard for modern media and EFI system partitions."},
			},
		},
		{
			ID:          "sectors_per_cluster",
			Name:        "Sectors per cluster",
			Description: "Cluster size in logical sectors (power of two). Larger clusters waste space on small files but keep the FAT small on big volumes. Leave on auto unless a consumer requires a value.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-s {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (by device size)"},
				{Value: "1", Label: "1 sector"},
				{Value: "2", Label: "2 sectors"},
				{Value: "4", Label: "4 sectors"},
				{Value: "8", Label: "8 sectors", Help: "4 KiB clusters with 512-byte sectors."},
				{Value: "16", Label: "16 sectors"},
				{Value: "32", Label: "32 sectors"},
				{Value: "64", Label: "64 sectors", Help: "32 KiB clusters with 512-byte sectors."},
				{Value: "128", Label: "128 sectors"},
			},
		},
		{
			ID:          "sector_size",
			Name:        "Logical sector size",
			Description: "Bytes per logical sector. Auto uses the device's sector size, which is almost always correct. Sizes other than 512 are poorly supported by other operating systems.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-S {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (device sector size)"},
				{Value: "512", Label: "512 bytes", Help: "Universal default."},
				{Value: "1024", Label: "1024 bytes"},
				{Value: "2048", Label: "2048 bytes"},
				{Value: "4096", Label: "4096 bytes"},
			},
		},
		{
			ID:          "volume_id",
			Name:        "Volume ID",
			Description: "FAT volume serial number as 8 hex digits, displayed as XXXX-XXXX. Random when unset. FAT has no UUID; this is what mount-by-UUID matches against.",
			Type:        KindString,
			Default:     "",
			Flag:        "-i {value}",
			Pattern:     `^[0-9a-fA-F]{8}$`,
			Placeholder: "xxxxxxxx",
		},
	},
}

var exfat = Schema{
	ID:          "exfat",
	Name:        "exFAT",
	Description: "Extended FAT for large cross-platform media: big SD cards and drives, files over 4 GiB. No permissions or journaling.",
	Binary:      "mkfs.exfat",
	ForceFlag:   "", // mkfs.exfat overwrites signatures unconditionally; see spec §9
	MinVersion:  "1.1",
	Options: []Option{
		{
			ID:          "label",
			Name:        "Volume label",
			Description: "Human-readable name for the filesystem. exFAT labels hold up to 11 characters.",
			Type:        KindString,
			Default:     "",
			Flag:        "-L {value}",
			MaxBytes:    44,
			Pattern:     `^[^\x00-\x1f]{0,11}$`,
			Placeholder: "(none)",
		},
		{
			ID:          "cluster_size",
			Name:        "Cluster size",
			Description: "Allocation unit size. Auto picks by device size (128 KiB on most SDXC-class media). Larger clusters speed up large-file workloads; smaller ones waste less space on small files.",
			LongHelp: `exFAT allocates space in clusters; every file occupies at least one. The
backend default scales with device size and follows the SD Association's
recommendation for card sizes (commonly 32 KiB up to 32 GiB and 128 KiB
above), which is what cameras and other appliances expect.

Pick a large cluster (512 KiB, 1 MiB) only for volumes holding exclusively
big files — video archives, disk images — where it reduces metadata
overhead. Pick a small one (4-32 KiB) for mixed content on large drives
where per-file waste matters. When unsure, leave on Auto.`,
			Type:    KindEnum,
			Default: "auto",
			Omit:    "auto",
			Flag:    "-c {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (by device size)", Help: "Scales with device size; matches SD Association recommendations."},
				{Value: "4K", Label: "4 KiB", Help: "Least waste for many small files."},
				{Value: "8K", Label: "8 KiB"},
				{Value: "16K", Label: "16 KiB"},
				{Value: "32K", Label: "32 KiB", Help: "Common on cards up to 32 GiB."},
				{Value: "64K", Label: "64 KiB"},
				{Value: "128K", Label: "128 KiB", Help: "Common on SDXC cards above 32 GiB."},
				{Value: "256K", Label: "256 KiB"},
				{Value: "512K", Label: "512 KiB"},
				{Value: "1M", Label: "1 MiB", Help: "Large-file-only volumes (video, images)."},
			},
		},
		{
			ID:          "boundary_align",
			Name:        "Boundary alignment",
			Description: "Aligns filesystem structures to this boundary, matching the flash erase block for less write amplification. Backend default is 1 MiB; 4-16 MiB suits many SD cards.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-b {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (1 MiB)"},
				{Value: "1M", Label: "1 MiB"},
				{Value: "4M", Label: "4 MiB"},
				{Value: "16M", Label: "16 MiB", Help: "Typical erase-block boundary on larger SD cards."},
			},
		},
	},
}

var f2fs = Schema{
	ID:           "f2fs",
	Name:         "F2FS",
	Description:  "Log-structured filesystem for NAND flash: SD cards, eMMC, SSDs. Android's data filesystem.",
	Binary:       "mkfs.f2fs",
	ForceFlag:    "-f", // force overwrite of an existing filesystem
	// No MinVersion: mkfs.f2fs offers no safe way to probe its version (see
	// internal/device/backend.go), so a floor could never be checked.
	MinSizeBytes: 52428800, // mkfs.f2fs needs ~50 MiB for its metadata segments
	Options: []Option{
		{
			ID:          "label",
			Name:        "Volume label",
			Description: "Human-readable name for the filesystem. Up to 512 bytes.",
			Type:        KindString,
			Default:     "",
			Flag:        "-l {value}",
			MaxBytes:    512,
			Pattern:     `^[^\x00-\x1f]*$`,
			Placeholder: "(none)",
		},
	},
}

var xfs = Schema{
	ID:           "xfs",
	Name:         "XFS",
	Description:  "High-performance journaling filesystem. Default on RHEL. Cannot be shrunk after creation.",
	Binary:       "mkfs.xfs",
	ForceFlag:    "-f",
	MinVersion:   "5.0.0",   // reflink era; see spec §8.3
	MinSizeBytes: 314572800, // mkfs.xfs refuses < 300 MiB on current xfsprogs
	Options: []Option{
		{
			ID:          "label",
			Name:        "Volume label",
			Description: "Human-readable name for the filesystem. XFS labels are limited to 12 bytes.",
			Type:        KindString,
			Default:     "",
			Flag:        "-L {value}",
			MaxBytes:    12,
			Pattern:     `^[^\x00-\x1f ]*$`,
			Placeholder: "(none)",
		},
		{
			ID:          "uuid",
			Name:        "UUID",
			Description: "Set a specific filesystem UUID instead of a random one.",
			Type:        KindString,
			Default:     "",
			Flag:        "-m uuid={value}",
			Pattern:     `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			Placeholder: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		},
		{
			ID:          "block_size",
			Name:        "Block size",
			Description: "Filesystem block size. Must not exceed the system page size (4 KiB on x86-64). Leave at default unless you know why.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-b size={value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (4096)"},
				{Value: "1024", Label: "1 KiB"},
				{Value: "2048", Label: "2 KiB"},
				{Value: "4096", Label: "4 KiB", Help: "Default."},
			},
		},
		{
			ID:          "inode_size",
			Name:        "Inode size",
			Description: "On-disk inode size in bytes. Default 512. Larger inodes hold more extended attributes inline. 256 is excluded: the v5 on-disk format's minimum is 512.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-i size={value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (512)"},
				{Value: "512", Label: "512 bytes", Help: "Default."},
				{Value: "1024", Label: "1024 bytes"},
				{Value: "2048", Label: "2048 bytes"},
			},
		},
		{
			ID:          "reflink",
			Name:        "Reflink (copy-on-write clones)",
			Description: "Enables reflink copies (cp --reflink) and deduplication support. Backend default is on. Turn off only for niche workloads where CoW metadata overhead matters.",
			Type:        KindBool,
			Default:     true,
			FlagFalse:   "-m reflink=0",
		},
		{
			ID:            "stripe_unit",
			Name:          "RAID stripe unit (su)",
			Description:   "Stripe unit (chunk size) of the underlying RAID array, e.g. 64k or 512k. Set together with stripe width on md/hardware RAID so XFS aligns allocations to stripes. Leave empty on plain disks.",
			Type:          KindSize,
			Default:       "",
			EmitAs:        "suffixed",
			CompositeOnly: true,
			Min:           i64(512),
			Max:           i64(1073741824),
		},
		{
			ID:            "stripe_width",
			Name:          "RAID stripe width (sw)",
			Description:   "Number of data disks in the RAID array (excluding parity). E.g. 4-disk RAID5 = 3, 4-disk RAID10 = 2.",
			Type:          KindInt,
			Default:       int64(0),
			Omit:          int64(0),
			CompositeOnly: true,
			Min:           i64(1),
			Max:           i64(1024),
		},
	},
	Composites: []Composite{
		{Flag: "-d su={stripe_unit},sw={stripe_width}", Requires: []string{"stripe_unit", "stripe_width"}},
	},
}

var btrfs = Schema{
	ID:           "btrfs",
	Name:         "Btrfs",
	Description:  "Copy-on-write filesystem with snapshots, checksums, and compression. Single-device mode only in cmkfs.",
	Binary:       "mkfs.btrfs",
	ForceFlag:    "-f",
	MinVersion:   "5.5",     // --csum introduced in btrfs-progs 5.5; see spec §8.3
	MinSizeBytes: 117440512, // mkfs.btrfs minimum ~114 MiB (mixed off)
	Options: []Option{
		{
			ID:          "label",
			Name:        "Volume label",
			Description: "Human-readable name for the filesystem. Up to 255 bytes.",
			Type:        KindString,
			Default:     "",
			Flag:        "-L {value}",
			MaxBytes:    255,
			Pattern:     `^[^\x00-\x1f]*$`,
			Placeholder: "(none)",
		},
		{
			ID:          "uuid",
			Name:        "UUID",
			Description: "Set a specific filesystem UUID instead of a random one.",
			Type:        KindString,
			Default:     "",
			Flag:        "-U {value}",
			Pattern:     `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			Placeholder: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		},
		{
			ID:          "data_profile",
			Name:        "Data profile",
			Description: "Allocation profile for file data. On a single device only 'single' (default) and 'dup' (two copies, halves capacity) are meaningful.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-d {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (single)"},
				{Value: "single", Label: "single", Help: "One copy of data. Default."},
				{Value: "dup", Label: "dup", Help: "Two copies of all data on the same device. Survives bad sectors, not disk loss. Halves capacity."},
			},
		},
		{
			ID:          "metadata_profile",
			Name:        "Metadata profile",
			Description: "Allocation profile for filesystem metadata. Backend default is 'dup' on most single devices — duplicated metadata protects against localized corruption.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-m {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (dup on most devices)", Help: "dup on most single devices, single on some SSD setups."},
				{Value: "single", Label: "single", Help: "One copy of metadata. Slightly more space/speed, less resilient."},
				{Value: "dup", Label: "dup", Help: "Two copies of metadata. Recommended."},
			},
		},
		{
			ID:          "nodesize",
			Name:        "Node size",
			Description: "Size of metadata B-tree nodes. Default 16 KiB. Larger nodes pack metadata better at the cost of lock contention on metadata-heavy workloads.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "-n {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (16 KiB)"},
				{Value: "16384", Label: "16 KiB", Help: "Default."},
				{Value: "32768", Label: "32 KiB"},
				{Value: "65536", Label: "64 KiB"},
			},
		},
		{
			ID:          "checksum",
			Name:        "Checksum algorithm",
			Description: "Algorithm for data and metadata checksums. crc32c (default) is fastest; xxhash is a strong fast alternative; sha256/blake2 are cryptographic but slower.",
			Type:        KindEnum,
			Default:     "auto",
			Omit:        "auto",
			Flag:        "--csum {value}",
			Values: []EnumValue{
				{Value: "auto", Label: "Auto (crc32c)"},
				{Value: "crc32c", Label: "crc32c", Help: "Default. Hardware-accelerated on modern CPUs."},
				{Value: "xxhash", Label: "xxhash", Help: "Stronger collision resistance than crc32c, still fast."},
				{Value: "sha256", Label: "sha256", Help: "Cryptographic. Noticeably slower."},
				{Value: "blake2", Label: "blake2", Help: "Cryptographic, faster than sha256."},
			},
		},
	},
}
