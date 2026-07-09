//go:build linux

package main

import (
	"errors"
	"os"
)

// requireRoot: cmkfs must run as root (spec §2); exit 4 otherwise.
func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("cmkfs must run as root (try: sudo cmkfs)")
	}
	return nil
}
