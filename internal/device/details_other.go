//go:build !linux

package device

// CollectDetails needs statfs and a real /sys; non-Linux builds exist only
// for the dev box, where the UI tests inject their own details. The sysfs
// traversal itself is portable and covered by details_test.go everywhere.
func CollectDetails(d Device) Details { return Details{} }
