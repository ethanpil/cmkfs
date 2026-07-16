//go:build !linux

package device

// CollectDetails needs statfs and sysfs; non-Linux builds exist only for
// tests, which inject their own details.
func CollectDetails(d Device) Details { return Details{} }
