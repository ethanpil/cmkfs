// Package safety implements every pre-format check from spec §9. Checks run
// at three points — device list, confirm-screen render, and FinalGate right
// before spawn — and only the last one is authoritative.
package safety

import (
	"strings"

	"github.com/ethanpil/cmkfs/internal/device"
)

// Severity of a finding.
type Severity int

const (
	Info Severity = iota
	Warning
	Blocker
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Blocker:
		return "blocker"
	}
	return "unknown"
}

// Finding is one safety check result.
type Finding struct {
	Severity Severity
	Code     string // stable identifier, used in tests
	Message  string // user-facing, includes specifics
}

// Report aggregates the findings for one target device.
type Report struct{ Findings []Finding }

// Blocked reports whether any finding is a Blocker.
func (r Report) Blocked() bool {
	for _, f := range r.Findings {
		if f.Severity == Blocker {
			return true
		}
	}
	return false
}

// NeedsForce reports whether the force flag must be injected: any
// SIGNATURE_* finding (never EXTRA_ARGS; spec §9).
func (r Report) NeedsForce() bool {
	for _, f := range r.Findings {
		if strings.HasPrefix(f.Code, "SIGNATURE_") {
			return true
		}
	}
	return false
}

// IsWholeDisk reports whether the target is an entire disk (WHOLE_DISK or
// WHOLE_DISK_BARE): drives schema.WholeDiskFlag injection (spec §9).
func (r Report) IsWholeDisk() bool {
	return r.Has("WHOLE_DISK") || r.Has("WHOLE_DISK_BARE")
}

// HasWarnings reports whether any finding is a Warning (drives the typed
// confirmation on Screen 4).
func (r Report) HasWarnings() bool {
	for _, f := range r.Findings {
		if f.Severity == Warning {
			return true
		}
	}
	return false
}

// Has reports whether a finding with the given code is present.
func (r Report) Has(code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// Blockers returns only the Blocker findings.
func (r Report) Blockers() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == Blocker {
			out = append(out, f)
		}
	}
	return out
}

// Fingerprint pins the device identity captured at confirmation time.
type Fingerprint struct {
	SizeBytes      int64
	FSType, FSUUID string
	PTType         string
}

// FingerprintOf captures the identity of a device.
func FingerprintOf(d device.Device) Fingerprint {
	return Fingerprint{
		SizeBytes: d.SizeBytes,
		FSType:    d.FSType,
		FSUUID:    d.UUID,
		PTType:    d.PTType,
	}
}
