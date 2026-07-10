package cmdgen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ethanpil/cmkfs/internal/schema"
)

func schemaByID(t *testing.T, id string) schema.Schema {
	t.Helper()
	for _, s := range schema.Schemas {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no schema %q", id)
	return schema.Schema{}
}

// TestBuildGolden is the golden table from spec §13.1(2): byte-exact argv
// and display strings.
func TestBuildGolden(t *testing.T) {
	cases := []struct {
		name        string
		schemaID    string
		values      map[string]any
		extra       []string
		device      string
		force       bool
		wholeDisk   bool
		wantArgv    []string
		wantDisplay string
	}{
		{
			name:        "ext4 all defaults",
			schemaID:    "ext4",
			device:      "/dev/loop9",
			wantArgv:    []string{"mkfs.ext4", "/dev/loop9"},
			wantDisplay: "mkfs.ext4 /dev/loop9",
		},
		{
			name:        "xfs all defaults",
			schemaID:    "xfs",
			device:      "/dev/loop9",
			wantArgv:    []string{"mkfs.xfs", "/dev/loop9"},
			wantDisplay: "mkfs.xfs /dev/loop9",
		},
		{
			name:        "btrfs all defaults",
			schemaID:    "btrfs",
			device:      "/dev/loop9",
			wantArgv:    []string{"mkfs.btrfs", "/dev/loop9"},
			wantDisplay: "mkfs.btrfs /dev/loop9",
		},
		{
			name:     "ext4 largefile4 example from spec section 7",
			schemaID: "ext4",
			values: map[string]any{
				"label":            "media",
				"usage_type":       "largefile4",
				"reserved_percent": int64(0),
				"journal":          true,
			},
			device:      "/dev/sdb1",
			wantArgv:    []string{"mkfs.ext4", "-L", "media", "-T", "largefile4", "-m", "0", "/dev/sdb1"},
			wantDisplay: "mkfs.ext4 -L media -T largefile4 -m 0 /dev/sdb1",
		},
		{
			name:        "ext4 journal disabled",
			schemaID:    "ext4",
			values:      map[string]any{"journal": false},
			device:      "/dev/sdb1",
			wantArgv:    []string{"mkfs.ext4", "-O", "^has_journal", "/dev/sdb1"},
			wantDisplay: "mkfs.ext4 -O '^has_journal' /dev/sdb1",
		},
		{
			name:        "ext4 label with a space stays one argv element",
			schemaID:    "ext4",
			values:      map[string]any{"label": "my disk"},
			device:      "/dev/sdb1",
			wantArgv:    []string{"mkfs.ext4", "-L", "my disk", "/dev/sdb1"},
			wantDisplay: "mkfs.ext4 -L 'my disk' /dev/sdb1",
		},
		{
			name:     "xfs su/sw composite present",
			schemaID: "xfs",
			values: map[string]any{
				"stripe_unit":  "64k",
				"stripe_width": int64(3),
			},
			device:      "/dev/md0p1",
			wantArgv:    []string{"mkfs.xfs", "-d", "su=64k,sw=3", "/dev/md0p1"},
			wantDisplay: "mkfs.xfs -d su=64k,sw=3 /dev/md0p1",
		},
		{
			name:        "xfs composite absent when both unset",
			schemaID:    "xfs",
			values:      map[string]any{"label": "data"},
			device:      "/dev/sdc1",
			wantArgv:    []string{"mkfs.xfs", "-L", "data", "/dev/sdc1"},
			wantDisplay: "mkfs.xfs -L data /dev/sdc1",
		},
		{
			name:        "xfs reflink disabled",
			schemaID:    "xfs",
			values:      map[string]any{"reflink": false},
			device:      "/dev/sdc1",
			wantArgv:    []string{"mkfs.xfs", "-m", "reflink=0", "/dev/sdc1"},
			wantDisplay: "mkfs.xfs -m reflink=0 /dev/sdc1",
		},
		{
			name:     "btrfs dup dup xxhash",
			schemaID: "btrfs",
			values: map[string]any{
				"data_profile":     "dup",
				"metadata_profile": "dup",
				"checksum":         "xxhash",
			},
			device:      "/dev/sdd1",
			wantArgv:    []string{"mkfs.btrfs", "-d", "dup", "-m", "dup", "--csum", "xxhash", "/dev/sdd1"},
			wantDisplay: "mkfs.btrfs -d dup -m dup --csum xxhash /dev/sdd1",
		},
		{
			name:        "ext4 force injection position",
			schemaID:    "ext4",
			values:      map[string]any{"label": "x"},
			device:      "/dev/sdb1",
			force:       true,
			wantArgv:    []string{"mkfs.ext4", "-F", "-L", "x", "/dev/sdb1"},
			wantDisplay: "mkfs.ext4 -F -L x /dev/sdb1",
		},
		{
			name:        "xfs force injection position",
			schemaID:    "xfs",
			device:      "/dev/sdb1",
			force:       true,
			wantArgv:    []string{"mkfs.xfs", "-f", "/dev/sdb1"},
			wantDisplay: "mkfs.xfs -f /dev/sdb1",
		},
		{
			name:        "btrfs force injection position",
			schemaID:    "btrfs",
			device:      "/dev/sdb1",
			force:       true,
			wantArgv:    []string{"mkfs.btrfs", "-f", "/dev/sdb1"},
			wantDisplay: "mkfs.btrfs -f /dev/sdb1",
		},
		{
			name:        "extra args appended after schema flags before device",
			schemaID:    "ext4",
			values:      map[string]any{"label": "x"},
			extra:       []string{"-E", "nodiscard", "token with space"},
			device:      "/dev/sdb1",
			wantArgv:    []string{"mkfs.ext4", "-L", "x", "-E", "nodiscard", "token with space", "/dev/sdb1"},
			wantDisplay: "mkfs.ext4 -L x -E nodiscard 'token with space' /dev/sdb1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := schemaByID(t, tc.schemaID)
			values := tc.values
			if values == nil {
				values = map[string]any{}
			}
			argv, display, err := Build(s, values, tc.extra, tc.device, tc.force, tc.wholeDisk)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !reflect.DeepEqual(argv, tc.wantArgv) {
				t.Errorf("argv:\n got %q\nwant %q", argv, tc.wantArgv)
			}
			if display != tc.wantDisplay {
				t.Errorf("display:\n got %q\nwant %q", display, tc.wantDisplay)
			}
		})
	}
}

// TestBuildForceAndWholeDiskFlags covers the spec §9 injection semantics for
// backends without a signature gate (ForceFlag "") and with a whole-disk
// override flag (WholeDiskFlag), using a synthetic schema.
func TestBuildForceAndWholeDiskFlags(t *testing.T) {
	s := schema.Schema{
		ID:            "t",
		Binary:        "mkfs.t",
		ForceFlag:     "",
		WholeDiskFlag: "-I",
	}
	cases := []struct {
		name             string
		force, wholeDisk bool
		wantArgv         []string
	}{
		{"force is a no-op without a ForceFlag", true, false, []string{"mkfs.t", "/dev/sdz"}},
		{"whole-disk flag injected", false, true, []string{"mkfs.t", "-I", "/dev/sdz"}},
		{"force no-op and whole-disk flag together", true, true, []string{"mkfs.t", "-I", "/dev/sdz"}},
		{"neither", false, false, []string{"mkfs.t", "/dev/sdz"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, _, err := Build(s, map[string]any{}, nil, "/dev/sdz", tc.force, tc.wholeDisk)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !reflect.DeepEqual(argv, tc.wantArgv) {
				t.Errorf("argv:\n got %q\nwant %q", argv, tc.wantArgv)
			}
		})
	}

	// Force flag comes before the whole-disk flag when both apply.
	forced := schema.Schema{ID: "t2", Binary: "mkfs.t2", ForceFlag: "-f", WholeDiskFlag: "-I"}
	argv, _, err := Build(forced, map[string]any{}, nil, "/dev/sdz", true, true)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{"mkfs.t2", "-f", "-I", "/dev/sdz"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv:\n got %q\nwant %q", argv, want)
	}
}

func TestBuildErrors(t *testing.T) {
	ext4 := schemaByID(t, "ext4")
	xfs := schemaByID(t, "xfs")

	cases := []struct {
		name   string
		schema schema.Schema
		values map[string]any
		extra  []string
		want   string
	}{
		{
			name:   "conflict violation usage_type plus bytes_per_inode",
			schema: ext4,
			values: map[string]any{"usage_type": "largefile", "bytes_per_inode": int64(4096)},
			want:   "conflicts",
		},
		{
			name:   "composite all-or-none violated",
			schema: xfs,
			values: map[string]any{"stripe_unit": "64k"},
			want:   "together, or none",
		},
		{
			name:   "label too long",
			schema: ext4,
			values: map[string]any{"label": strings.Repeat("x", 17)},
			want:   "exceeds 16 bytes",
		},
		{
			name:   "int out of bounds",
			schema: ext4,
			values: map[string]any{"reserved_percent": int64(51)},
			want:   "between 0 and 50",
		},
		{
			name:   "bad uuid pattern",
			schema: ext4,
			values: map[string]any{"uuid": "not-a-uuid"},
			want:   "invalid value",
		},
		{
			name:   "wrong value type",
			schema: ext4,
			values: map[string]any{"label": int64(3)},
			want:   "expected string",
		},
		{
			name:   "guardrail empty token",
			schema: ext4,
			extra:  []string{""},
			want:   "empty or whitespace-only",
		},
		{
			name:   "guardrail whitespace token",
			schema: ext4,
			extra:  []string{"   "},
			want:   "empty or whitespace-only",
		},
		{
			name:   "guardrail newline token",
			schema: ext4,
			extra:  []string{"a\nb"},
			want:   "newline",
		},
		{
			name:   "guardrail token equals device",
			schema: ext4,
			extra:  []string{"/dev/sdb1"},
			want:   "must not be the target device path",
		},
		{
			name:   "guardrail token starts with /dev/",
			schema: ext4,
			extra:  []string{"/dev/sdz"},
			want:   "must not be a /dev/ path",
		},
		{
			name:   "guardrail token equals force flag",
			schema: ext4,
			extra:  []string{"-F"},
			want:   "must not be the force flag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := tc.values
			if values == nil {
				values = map[string]any{}
			}
			argv, _, err := Build(tc.schema, values, tc.extra, "/dev/sdb1", false, false)
			if err == nil {
				t.Fatalf("expected error containing %q, got argv %q", tc.want, argv)
			}
			if argv != nil {
				t.Errorf("argv must be nil on error, got %q", argv)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

// TestValidateSizeSingleBound: a KindSize option with only one bound must
// produce an error, not a nil-pointer panic on the missing bound.
func TestValidateSizeSingleBound(t *testing.T) {
	min := int64(512)
	s := schema.Schema{ID: "t", Binary: "mkfs.t"}
	minOnly := schema.Option{ID: "sz", Name: "Size", Type: schema.KindSize, Default: "", Min: &min}
	if err := ValidateValue(s, minOnly, "1"); err == nil || !strings.Contains(err.Error(), "512") {
		t.Fatalf("want bound error, got %v", err)
	}
	max := int64(4096)
	maxOnly := schema.Option{ID: "sz", Name: "Size", Type: schema.KindSize, Default: "", Max: &max}
	if err := ValidateValue(s, maxOnly, "8k"); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("want bound error, got %v", err)
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"512", 512, false},
		{"64k", 65536, false},
		{"64K", 65536, false},
		{"1m", 1048576, false},
		{"2G", 2147483648, false},
		{"", 0, true},
		{"12kb", 0, true},
		{"-5", 0, true},
		{"1.5g", 0, true},
		{"k", 0, true},
		{"99999999999999999999", 0, true},
		{"9223372036854775807g", 0, true}, // overflow
	}
	for _, tc := range cases {
		got, err := ParseSize(tc.in)
		if tc.wantErr != (err != nil) {
			t.Errorf("ParseSize(%q): err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mkfs.ext4", "mkfs.ext4"},
		{"/dev/sdb1", "/dev/sdb1"},
		{"su=64k,sw=3", "su=64k,sw=3"},
		{"my disk", "'my disk'"},
		{"^has_journal", "'^has_journal'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
		{"a;b", "'a;b'"},
		{"$HOME", "'$HOME'"},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestBuildDeterminism: identical inputs produce byte-identical output
// (spec §7).
func TestBuildDeterminism(t *testing.T) {
	s := schemaByID(t, "ext4")
	values := map[string]any{"label": "media", "usage_type": "largefile4"}
	a1, d1, err := Build(s, values, []string{"-E", "nodiscard"}, "/dev/sdb1", true, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		a2, d2, err := Build(s, values, []string{"-E", "nodiscard"}, "/dev/sdb1", true, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a1, a2) || d1 != d2 {
			t.Fatalf("non-deterministic output: %q vs %q", d1, d2)
		}
	}
}
