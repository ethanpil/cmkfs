package device

import "fmt"

var sizeSuffixes = []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// sizeParts reduces a byte count to a value and suffix index, clamped to the
// largest known suffix so absurd sizes can never index out of range.
func sizeParts(b int64) (float64, int) {
	const unit = 1024
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < len(sizeSuffixes)-1; n /= unit {
		div *= unit
		exp++
	}
	return float64(b) / float64(div), exp
}

// HumanSize renders a byte count in binary units, e.g. "300 MiB", "1.5 GiB".
func HumanSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	v, exp := sizeParts(b)
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d %s", int64(v), sizeSuffixes[exp])
	}
	return fmt.Sprintf("%.1f %s", v, sizeSuffixes[exp])
}

// HumanSizeCompact is the short table form, e.g. "1.5G", "500G".
func HumanSizeCompact(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	v, exp := sizeParts(b)
	suffix := sizeSuffixes[exp][:1]
	if v >= 100 {
		return fmt.Sprintf("%.0f%s", v, suffix)
	}
	return fmt.Sprintf("%.1f%s", v, suffix)
}
