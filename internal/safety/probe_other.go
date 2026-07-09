//go:build !linux

package safety

// ProbeExclusive is a no-op on non-Linux platforms; cmkfs only runs on
// Linux (spec §2), this stub exists so the package compiles everywhere for
// development and unit tests.
func ProbeExclusive(path string) error { return nil }
