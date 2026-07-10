// Package demodata wires the real cmkfs UI to in-memory sample devices so it
// can be driven with no root, no lsblk, and no real block devices. It backs
// the README GIF generator (internal/gendemo); nothing here ships in the
// cmkfs binary.
package demodata

import (
	"context"
	"os"

	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/executor"
	"github.com/ethanpil/cmkfs/internal/safety"
	"github.com/ethanpil/cmkfs/internal/schema"
	"github.com/ethanpil/cmkfs/internal/ui"
)

// SampleDevices is a small, realistic device tree. /dev/sda1 carries an
// existing ext4 signature, which drives the safety story in the demo: a
// typed confirmation and an injected force flag.
func SampleDevices() []device.Device {
	return []device.Device{
		{Path: "/dev/sda", KName: "sda", MajMin: "8:0", Type: "disk", SizeBytes: 500107862016, PTType: "gpt", Model: "Samsung 870 EVO", Transport: "sata", Children: []string{"/dev/sda1"}},
		{Path: "/dev/sda1", KName: "sda1", MajMin: "8:1", Type: "part", SizeBytes: 500105764864, FSType: "ext4", Label: "olddata", UUID: "3f5c9e2a-1111-4a5b-9c3d-abcdef012345", Model: "Samsung 870 EVO", Parent: "/dev/sda"},
		{Path: "/dev/sdb", KName: "sdb", MajMin: "8:16", Type: "disk", SizeBytes: 2000398934016, PTType: "gpt", Model: "WD Blue 2TB", Transport: "sata", Rotational: true, Children: []string{"/dev/sdb1"}},
		{Path: "/dev/sdb1", KName: "sdb1", MajMin: "8:17", Type: "part", SizeBytes: 2000397795328, FSType: "xfs", Label: "media", UUID: "5a4b3c2d-3333-4c7d-9e5f-0123456789ab", Model: "WD Blue 2TB", Parent: "/dev/sdb"},
		{Path: "/dev/sdc", KName: "sdc", MajMin: "8:32", Type: "disk", SizeBytes: 31268536320, Model: "SanDisk Ultra", Transport: "usb", Removable: true},
	}
}

func fakeRun(ctx context.Context, argv []string, gate func() (safety.Report, bool)) <-chan executor.Event {
	ch := make(chan executor.Event, 32)
	go func() {
		defer close(ch)
		if gate != nil {
			if report, ok := gate(); !ok {
				ch <- executor.Event{Done: true, Exit: -1, Gate: &report}
				return
			}
		}
		for _, line := range []string{
			"mke2fs 1.47.0 (5-Feb-2023)",
			"Creating filesystem with 122096646 4k blocks and 30531584 inodes",
			"Filesystem UUID: 8f2b1c4a-7d9e-4f10-b2a6-5c3e9d1f0a72",
			"Superblock backups stored on blocks:",
			"        32768, 98304, 163840, 229376, 294912, 819200, 884736",
			"Allocating group tables: done",
			"Writing inode tables: done",
			"Creating journal (262144 blocks): done",
			"Writing superblocks and filesystem accounting information: done",
		} {
			ch <- executor.Event{Line: line}
		}
		ch <- executor.Event{Done: true, Exit: 0}
	}()
	return ch
}

// Config returns a ui.Config wired to the sample data above.
func Config() ui.Config {
	devs := SampleDevices()
	backends := map[string]device.Backend{}
	for _, s := range schema.Schemas {
		backends[s.Binary] = device.Backend{Binary: s.Binary, Path: "/sbin/" + s.Binary, Version: "9.9"}
	}
	// An empty proc/sys root means no live mounts, swaps, or holders, so
	// findings come only from the sample device fields (signatures, size,
	// removable) — enough to show the safety flow without a real system.
	root, _ := os.MkdirTemp("", "cmkfs-demo")
	return ui.Config{
		Schemas:        schema.Schemas,
		Backends:       backends,
		Sys:            safety.System{ProcRoot: root, SysRoot: root, ProbeExcl: func(string) error { return nil }},
		Discover:       func(bool) ([]device.Device, error) { return SampleDevices(), nil },
		InitialDevices: devs,
		Run:            fakeRun,
	}
}
