//go:build linux

package safety

import (
	"errors"
	"syscall"
)

// ProbeExclusive opens the device O_RDONLY|O_EXCL and closes it immediately
// (probe-and-release; spec §11). On a block device, O_EXCL fails with EBUSY
// when some process holds it exclusively or the kernel considers it claimed.
// The fd must never be held across the spawn, or the backend's own busy
// check would fail against us.
func ProbeExclusive(path string) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_EXCL|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.EBUSY) {
			return ErrBusy
		}
		return err
	}
	syscall.Close(fd)
	return nil
}
