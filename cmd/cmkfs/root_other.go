//go:build !linux

package main

import "errors"

// cmkfs formats Linux block devices; there is nothing it can do elsewhere
// (spec §2).
func requireRoot() error {
	return errors.New("cmkfs only runs on Linux")
}
