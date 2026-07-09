package safety

import (
	"fmt"

	"github.com/ethanpil/cmkfs/internal/device"
)

// Gate carries everything FinalGate needs to re-run the full check set
// immediately before the executor spawns the backend (spec §9 point 3).
type Gate struct {
	Sys          System
	Discover     func(showLoop bool) ([]device.Device, error)
	ShowLoop     bool
	MinSizeBytes int64
	FSName       string
	ExtraArgs    int
}

// FinalGate re-enumerates the single device, re-runs every check (including
// the O_EXCL probe and a re-read of the sysfs ro flag), and compares the
// live fingerprint against the one captured at confirmation. ok is false on
// any Blocker, any Warning absent from the confirmed report, or a
// fingerprint mismatch (finding CHANGED_UNDER_US). Nothing ever executes
// against a stale confirmation.
func (g Gate) FinalGate(path string, confirmed Report, fp Fingerprint) (Report, bool) {
	blocked := func(msg string) (Report, bool) {
		return Report{Findings: []Finding{{Severity: Blocker, Code: "CHANGED_UNDER_US", Message: msg}}}, false
	}
	devs, err := g.Discover(g.ShowLoop)
	if err != nil {
		return blocked(fmt.Sprintf("cannot re-enumerate devices: %v", err))
	}
	var dev *device.Device
	for i := range devs {
		if devs[i].Path == path {
			dev = &devs[i]
			break
		}
	}
	if dev == nil {
		return blocked(fmt.Sprintf("%s disappeared since you confirmed. Review and confirm again.", path))
	}

	r := g.Sys.Check(Params{
		Device:       *dev,
		All:          devs,
		MinSizeBytes: g.MinSizeBytes,
		FSName:       g.FSName,
		ExtraArgs:    g.ExtraArgs,
		Probe:        true,
	})

	live := FingerprintOf(*dev)
	if live != fp {
		r.Findings = append(r.Findings, Finding{
			Severity: Blocker,
			Code:     "CHANGED_UNDER_US",
			Message:  fmt.Sprintf("%s changed since you confirmed (%s). Review and confirm again.", path, fingerprintDiff(fp, live)),
		})
	}

	if r.Blocked() {
		return r, false
	}
	// Any Warning that was not in the confirmed report invalidates the
	// confirmation: it is only valid for the world it was given in.
	confirmedCodes := map[string]bool{}
	for _, f := range confirmed.Findings {
		confirmedCodes[f.Code] = true
	}
	for _, f := range r.Findings {
		if f.Severity == Warning && !confirmedCodes[f.Code] {
			return r, false
		}
	}
	return r, true
}

func fingerprintDiff(was, now Fingerprint) string {
	orNone := func(s string) string {
		if s == "" {
			return "none"
		}
		return s
	}
	switch {
	case was.FSType != now.FSType:
		return fmt.Sprintf("was %s, now %s", orNone(was.FSType), orNone(now.FSType))
	case was.FSUUID != now.FSUUID:
		return "filesystem UUID changed"
	case was.SizeBytes != now.SizeBytes:
		return fmt.Sprintf("size was %s, now %s", humanSize(was.SizeBytes), humanSize(now.SizeBytes))
	case was.PTType != now.PTType:
		return fmt.Sprintf("partition table was %s, now %s", orNone(was.PTType), orNone(now.PTType))
	}
	return "device identity changed"
}
