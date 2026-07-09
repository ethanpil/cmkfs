package device

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Backend is the probe result for one mkfs binary (spec §8.3).
type Backend struct {
	Binary  string
	Path    string // "" when not found on $PATH
	Version string // "" when the banner was unparseable
}

func (b Backend) Found() bool { return b.Path != "" }

// versionProbes maps each backend binary to its version flag and the regex
// that extracts the version from the banner. This lives in Go, not in the
// schema (spec rule §5.1).
var versionProbes = map[string]struct {
	args []string
	re   *regexp.Regexp
}{
	"mkfs.ext4":  {[]string{"-V"}, regexp.MustCompile(`mke2fs (\d+(?:\.\d+)+)`)},
	"mkfs.xfs":   {[]string{"-V"}, regexp.MustCompile(`mkfs\.xfs version (\d+(?:\.\d+)*)`)},
	"mkfs.btrfs": {[]string{"--version"}, regexp.MustCompile(`btrfs-progs v(\d+(?:\.\d+)*)`)},
}

// ParseVersionBanner extracts the version from a backend's version banner.
// Returns "" when the banner is unparseable (soft warning path, spec §8.3).
func ParseVersionBanner(binary, banner string) string {
	p, ok := versionProbes[binary]
	if !ok {
		return ""
	}
	m := p.re.FindStringSubmatch(banner)
	if m == nil {
		return ""
	}
	return m[1]
}

// ProbeBackend looks up one backend on $PATH and, if present, runs its
// version flag once and parses the banner.
func ProbeBackend(binary string) Backend {
	b := Backend{Binary: binary}
	path, err := exec.LookPath(binary)
	if err != nil {
		return b
	}
	b.Path = path
	p, ok := versionProbes[binary]
	if !ok {
		return b
	}
	// mkfs.ext4 -V writes its banner to stderr; capture both streams.
	out, _ := exec.Command(path, p.args...).CombinedOutput()
	b.Version = ParseVersionBanner(binary, string(out))
	return b
}

// CompareVersions compares two dotted numeric versions: -1, 0, or 1.
// Missing components count as zero.
func CompareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}
