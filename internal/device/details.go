package device

// MountUsage is statfs-derived usage for one mountpoint.
type MountUsage struct {
	Mountpoint string
	TotalBytes int64
	FreeBytes  int64
}

// Details holds extra per-device information gathered on demand for the
// info screen. Every field is best-effort: zero values mean "unknown".
type Details struct {
	Mounts            []MountUsage
	LogicalBlockSize  int64
	PhysicalBlockSize int64
	TempCelsius       float64
	HasTemp           bool
}
