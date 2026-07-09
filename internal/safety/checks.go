package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ethanpil/cmkfs/internal/device"
)

// ErrBusy is returned by an O_EXCL probe when the device is exclusively held.
var ErrBusy = errors.New("device exclusively held")

// System carries the roots the checks read from, so unit tests can point
// them at testdata trees (spec §13.1(3)). Zero values mean the real /proc
// and /sys; ProbeExcl nil means the probe is skipped.
type System struct {
	ProcRoot  string
	SysRoot   string
	ProbeExcl func(path string) error
}

func (s System) procRoot() string {
	if s.ProcRoot == "" {
		return "/proc"
	}
	return s.ProcRoot
}

func (s System) sysRoot() string {
	if s.SysRoot == "" {
		return "/sys"
	}
	return s.SysRoot
}

// Params selects the target and the context for one check run.
type Params struct {
	Device       device.Device
	All          []device.Device
	MinSizeBytes int64  // schema's minimum; 0 = no filesystem picked yet
	FSName       string // schema display name, used in the TOO_SMALL message
	ExtraArgs    int    // number of extra-argument tokens (EXTRA_ARGS warning)
	Probe        bool   // run the O_EXCL probe (Screen 4 render and FinalGate)
}

type mountEntry struct {
	MajMin     string
	Mountpoint string
	FSType     string
	Source     string
}

// parseMountinfo parses /proc/self/mountinfo lines:
//
//	36 35 8:17 / /mnt/data rw,relatime shared:1 - ext4 /dev/sdb1 rw
func parseMountinfo(data string) []mountEntry {
	var out []mountEntry
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		e := mountEntry{
			MajMin:     fields[2],
			Mountpoint: unescapeMount(fields[4]),
		}
		if sep >= 0 && len(fields) > sep+2 {
			e.FSType = fields[sep+1]
			e.Source = unescapeMount(fields[sep+2])
		}
		out = append(out, e)
	}
	return out
}

// unescapeMount decodes the octal escapes mountinfo uses (\040 = space etc.).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (s System) mountinfo() []mountEntry {
	data, err := os.ReadFile(filepath.Join(s.procRoot(), "self", "mountinfo"))
	if err != nil {
		return nil
	}
	return parseMountinfo(string(data))
}

// swapSources returns the device paths listed in /proc/swaps.
func (s System) swapSources() []string {
	data, err := os.ReadFile(filepath.Join(s.procRoot(), "swaps"))
	if err != nil {
		return nil
	}
	var out []string
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 { // header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			out = append(out, unescapeMount(fields[0]))
		}
	}
	return out
}

// holders lists /sys/class/block/<kname>/holders/ entries.
func (s System) holders(kname string) []string {
	entries, err := os.ReadDir(filepath.Join(s.sysRoot(), "class", "block", kname, "holders"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// sysfsRO re-reads /sys/class/block/<kname>/ro; -1 when unreadable.
func (s System) sysfsRO(kname string) int {
	data, err := os.ReadFile(filepath.Join(s.sysRoot(), "class", "block", kname, "ro"))
	if err != nil {
		return -1
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return v
}

// knameForMajMin resolves a major:minor to a kernel name by scanning
// /sys/class/block/*/dev. Returns "" when not found.
func (s System) knameForMajMin(majmin string) string {
	dir := filepath.Join(s.sysRoot(), "class", "block")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "dev"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == majmin {
			return e.Name()
		}
	}
	return ""
}

// relatives collects the target plus its ancestors and descendants from the
// device tree. Transitivity matters for holder and system-disk checks.
type relatives struct {
	self        device.Device
	ancestors   []device.Device
	descendants []device.Device
}

func (r relatives) all() []device.Device {
	out := []device.Device{r.self}
	out = append(out, r.ancestors...)
	out = append(out, r.descendants...)
	return out
}

func relativesOf(dev device.Device, all []device.Device) relatives {
	byPath := map[string]device.Device{}
	for _, d := range all {
		byPath[d.Path] = d
	}
	r := relatives{self: dev}
	for p := dev.Parent; p != ""; {
		a, ok := byPath[p]
		if !ok {
			break
		}
		r.ancestors = append(r.ancestors, a)
		p = a.Parent
	}
	var walk func(d device.Device)
	walk = func(d device.Device) {
		for _, cp := range d.Children {
			c, ok := byPath[cp]
			if !ok {
				continue
			}
			r.descendants = append(r.descendants, c)
			walk(c)
		}
	}
	walk(dev)
	return r
}

// systemMountpoints are the mounts whose backing devices are protected
// (SYSTEM_DISK, spec §9).
var systemMountpoints = map[string]bool{
	"/": true, "/boot": true, "/boot/efi": true, "/usr": true,
}

// Check runs every device-level safety check and returns the aggregated
// report. Findings appear in the order of the table in spec §9.
func (s System) Check(p Params) Report {
	var r Report
	add := func(sev Severity, code, msg string) {
		r.Findings = append(r.Findings, Finding{Severity: sev, Code: code, Message: msg})
	}
	dev := p.Device
	rel := relativesOf(dev, p.All)
	mounts := s.mountinfo()

	// MOUNTED: the device or, for a whole disk, any child partition appears
	// in mountinfo. Verified against mountinfo directly, not lsblk.
	selfAndBelow := append([]device.Device{dev}, rel.descendants...)
	for _, d := range selfAndBelow {
		for _, m := range mounts {
			if m.Mountpoint == "" {
				continue
			}
			if (d.MajMin != "" && m.MajMin == d.MajMin) || (m.Source != "" && m.Source == d.Path) {
				add(Blocker, "MOUNTED", fmt.Sprintf("%s is mounted at %s. Unmount it first.", d.Path, m.Mountpoint))
			}
		}
	}

	// ACTIVE_SWAP: device or any child appears in /proc/swaps.
	swaps := s.swapSources()
	for _, d := range selfAndBelow {
		for _, sw := range swaps {
			if sw == d.Path || sw == "/dev/"+d.KName {
				add(Blocker, "ACTIVE_SWAP", fmt.Sprintf("%s is active swap. Run swapoff first.", d.Path))
			}
		}
	}

	// READ_ONLY: lsblk RO=1, or a live sysfs re-read says so.
	if dev.ReadOnly || s.sysfsRO(dev.KName) == 1 {
		add(Blocker, "READ_ONLY", fmt.Sprintf("%s is read-only.", dev.Path))
	}

	// IN_USE_HOLDER: non-empty holders/ for the device, any ancestor, or any
	// descendant (catches LVM PVs in active VGs, open dm-crypt mappings, md
	// members, multipath path devices).
	byKName := map[string]device.Device{}
	for _, d := range p.All {
		byKName[d.KName] = d
	}
	for _, d := range rel.all() {
		for _, h := range s.holders(d.KName) {
			desc := h
			if hd, ok := byKName[h]; ok {
				desc = fmt.Sprintf("%s (%s)", h, hd.Type)
			}
			add(Blocker, "IN_USE_HOLDER", fmt.Sprintf("%s is in use by %s. Close it first.", d.Path, desc))
		}
	}

	// DEVICE_BUSY: O_RDONLY|O_EXCL probe, probe-and-release.
	if p.Probe && s.ProbeExcl != nil {
		if err := s.ProbeExcl(dev.Path); errors.Is(err, ErrBusy) {
			add(Blocker, "DEVICE_BUSY", fmt.Sprintf("%s is exclusively held by another process.", dev.Path))
		}
	}

	// EXTRA_ARGS: never triggers force injection, always forces typed
	// confirmation.
	if p.ExtraArgs > 0 {
		plural := "s"
		if p.ExtraArgs == 1 {
			plural = ""
		}
		add(Warning, "EXTRA_ARGS", fmt.Sprintf("%d unsupported extra argument%s will be passed to mkfs unvalidated.", p.ExtraArgs, plural))
	}

	// SYSTEM_DISK: target is, or is an ancestor/descendant of, the device
	// backing /, /boot, /boot/efi, or /usr.
	sysKNames := map[string]bool{}
	for _, m := range mounts {
		if !systemMountpoints[m.Mountpoint] {
			continue
		}
		matched := false
		for _, d := range p.All {
			if (d.MajMin != "" && d.MajMin == m.MajMin) || (m.Source != "" && d.Path == m.Source) {
				sysKNames[d.KName] = true
				matched = true
			}
		}
		if !matched {
			if kn := s.knameForMajMin(m.MajMin); kn != "" {
				sysKNames[kn] = true
			}
		}
	}
	for _, d := range rel.all() {
		if sysKNames[d.KName] {
			add(Blocker, "SYSTEM_DISK", fmt.Sprintf("%s contains the running system's root filesystem.", dev.Path))
			break
		}
	}

	// WHOLE_DISK / WHOLE_DISK_BARE.
	if dev.Type == "disk" {
		if len(dev.Children) > 0 {
			plural := "s"
			if len(dev.Children) == 1 {
				plural = ""
			}
			add(Warning, "WHOLE_DISK", fmt.Sprintf("%s is an entire disk containing %d partition%s. All of them will be destroyed.", dev.Path, len(dev.Children), plural))
		} else {
			add(Info, "WHOLE_DISK_BARE", "Formatting the whole disk without a partition table.")
		}
	}

	// SIGNATURE_FS.
	if dev.FSType != "" {
		detail := ""
		if dev.Label != "" {
			detail = fmt.Sprintf(" (label '%s', UUID %s)", dev.Label, dev.UUID)
		} else if dev.UUID != "" {
			detail = fmt.Sprintf(" (UUID %s)", dev.UUID)
		}
		add(Warning, "SIGNATURE_FS", fmt.Sprintf("Existing %s filesystem%s will be destroyed.", dev.FSType, detail))
	}

	// SIGNATURE_PTABLE: whole-disk target with a partition table.
	if dev.Type == "disk" && dev.PTType != "" {
		add(Warning, "SIGNATURE_PTABLE", fmt.Sprintf("Existing %s partition table will be destroyed.", dev.PTType))
	}

	// TOO_SMALL: checked at filesystem-pick time.
	if p.MinSizeBytes > 0 && dev.SizeBytes < p.MinSizeBytes {
		add(Blocker, "TOO_SMALL", fmt.Sprintf("%s requires at least %s; %s is %s.", p.FSName, humanSize(p.MinSizeBytes), dev.Path, humanSize(dev.SizeBytes)))
	}

	// REMOVABLE.
	if dev.Removable {
		tran := strings.ToUpper(dev.Transport)
		if tran == "" {
			tran = "removable"
		}
		add(Info, "REMOVABLE", fmt.Sprintf("Removable device (%s). Double-check it's the right one.", tran))
	}

	return r
}

// humanSize renders a byte count in binary units.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	v := float64(b) / float64(div)
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}[exp]
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d %s", int64(v), suffix)
	}
	return fmt.Sprintf("%.1f %s", v, suffix)
}
