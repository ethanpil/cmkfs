package cmdgen

import (
	"strings"
	"testing"

	"github.com/ethanpil/cmkfs/internal/schema"
)

// FuzzBuild fuzzes the single function standing between user-typed input and
// a destructive command line (spec §13.3). Invariants: Build never panics;
// on nil error the device path is the final argv element and appears exactly
// once, no argv element is empty or contains an unsubstituted placeholder,
// and the force flag appears at its injection position iff force was passed;
// on error, argv is nil.
func FuzzBuild(f *testing.F) {
	// Seed corpus: representative values across kinds, extras, and edge inputs.
	f.Add("ext4", "media", "largefile4", int64(0), true, "64k", int64(3), "-E", "nodiscard", "/dev/loop9", false, false)
	f.Add("xfs", "data", "auto", int64(-1), true, "512k", int64(2), "", "", "/dev/sdb1", true, false)
	f.Add("btrfs", "my disk", "dup", int64(5), false, "", int64(0), "--mixed", "", "/dev/nvme0n1p2", false, true)
	f.Add("ext4", "", "auto", int64(-1), true, "", int64(0), "", "", "/dev/loop0", true, true)
	f.Add("ext4", "{value}", "small", int64(50), false, "1g", int64(1024), "-F", "/dev/sda", "/dev/sda", true, false)
	f.Add("xfs", "it's", "huge", int64(9999), true, "0", int64(-1), " ", "a\nb", "/dev/md0", false, true)

	f.Fuzz(func(t *testing.T, schemaID, label, usage string, resInt int64, boolVal bool,
		sizeVal string, intVal int64, extra1, extra2, device string, force, wholeDisk bool) {

		var s schema.Schema
		found := false
		for _, c := range schema.Schemas {
			if c.ID == schemaID {
				s, found = c, true
			}
		}
		if !found {
			s = schema.Schemas[int(intVal%int64(len(schema.Schemas))+int64(len(schema.Schemas)))%len(schema.Schemas)]
		}

		// Assemble a values map hitting each option kind that exists in the
		// schema, plus some deliberately mistyped entries via raw strings.
		values := map[string]any{}
		for _, o := range s.Options {
			switch o.Type {
			case schema.KindBool:
				values[o.ID] = boolVal
			case schema.KindEnum:
				values[o.ID] = usage
			case schema.KindInt:
				values[o.ID] = resInt
			case schema.KindString:
				values[o.ID] = label
			case schema.KindSize:
				values[o.ID] = sizeVal
			}
		}

		var extra []string
		if extra1 != "" {
			extra = append(extra, extra1)
		}
		if extra2 != "" {
			extra = append(extra, extra2)
		}

		argv, display, err := Build(s, values, extra, device, force, wholeDisk)
		if err != nil {
			if argv != nil {
				t.Fatalf("argv must be nil on error, got %q (err %v)", argv, err)
			}
			return
		}
		if len(argv) < 2 {
			t.Fatalf("argv too short: %q", argv)
		}
		if argv[0] != s.Binary {
			t.Fatalf("argv[0] = %q, want %q", argv[0], s.Binary)
		}
		if argv[len(argv)-1] != device {
			t.Fatalf("device %q is not the final argv element: %q", device, argv)
		}
		// "{value}" is the flag-template placeholder, but it is also a
		// perfectly legal user string: a label, a device path, or an extra
		// argument may contain it verbatim, and validation neither does nor
		// should reject it (there is no shell; the token reaches mkfs as-is).
		// Enum and size values equal to it are rejected by ValidateValue, so
		// the only argv carriers are these free-form inputs. When one of them
		// carries it, a "{value}" in argv is genuine content, not an
		// unsubstituted template.
		freeformHasPlaceholder := strings.Contains(label, "{value}") ||
			strings.Contains(device, "{value}") ||
			strings.Contains(extra1, "{value}") ||
			strings.Contains(extra2, "{value}")

		devCount := 0
		for _, a := range argv {
			if a == device {
				devCount++
			}
			if a == "" {
				t.Fatalf("empty argv element in %q", argv)
			}
			if !freeformHasPlaceholder && strings.Contains(a, "{value}") {
				t.Fatalf("unsubstituted {value} in %q", argv)
			}
		}
		for _, c := range s.Composites {
			for _, ref := range c.Requires {
				for _, a := range argv {
					if strings.Contains(a, "{"+ref+"}") {
						t.Fatalf("unsubstituted {%s} in %q", ref, argv)
					}
				}
			}
		}
		if devCount != 1 {
			t.Fatalf("device appears %d times in %q", devCount, argv)
		}
		// Force fields (iff force and the schema has a ForceFlag), then
		// whole-disk fields (iff wholeDisk and the schema has a
		// WholeDiskFlag), immediately after argv[0].
		var wantPrefix []string
		if force {
			wantPrefix = append(wantPrefix, strings.Fields(s.ForceFlag)...)
		}
		if wholeDisk {
			wantPrefix = append(wantPrefix, strings.Fields(s.WholeDiskFlag)...)
		}
		if len(argv) < 1+len(wantPrefix)+1 {
			t.Fatalf("injected flags missing: %q", argv)
		}
		for i, ff := range wantPrefix {
			if argv[1+i] != ff {
				t.Fatalf("injected flags not in position after argv[0]: %q, want prefix %q", argv, wantPrefix)
			}
		}
		if display == "" {
			t.Fatal("empty display string")
		}
	})
}
