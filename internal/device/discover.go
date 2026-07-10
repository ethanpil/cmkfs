package device

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

// Columns requested from lsblk. KNAME, MAJ:MIN and PTTYPE are needed by the
// safety subsystem (sysfs holder walks, mountinfo resolution, SIGNATURE_PTABLE).
const lsblkColumns = "NAME,KNAME,MAJ:MIN,TYPE,SIZE,FSTYPE,LABEL,UUID,PTTYPE,MOUNTPOINTS,MODEL,SERIAL,TRAN,ROTA,RO,RM,PKNAME"

// flexBool tolerates both modern lsblk JSON booleans and the "0"/"1" strings
// older util-linux versions emit.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	switch s {
	case "true", "1":
		*b = true
	case "false", "0", "null", "":
		*b = false
	default:
		return fmt.Errorf("cannot parse %q as bool", s)
	}
	return nil
}

// flexInt64 tolerates numeric and quoted numeric values.
type flexInt64 int64

func (n *flexInt64) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot parse %q as int64", s)
	}
	*n = flexInt64(v)
	return nil
}

type lsblkNode struct {
	Name        string      `json:"name"`
	KName       string      `json:"kname"`
	MajMin      string      `json:"maj:min"`
	Type        string      `json:"type"`
	Size        flexInt64   `json:"size"`
	FSType      *string     `json:"fstype"`
	Label       *string     `json:"label"`
	UUID        *string     `json:"uuid"`
	PTType      *string     `json:"pttype"`
	Mountpoints []*string   `json:"mountpoints"`
	Model       *string     `json:"model"`
	Serial      *string     `json:"serial"`
	Tran        *string     `json:"tran"`
	Rota        flexBool    `json:"rota"`
	RO          flexBool    `json:"ro"`
	RM          flexBool    `json:"rm"`
	PKName      *string     `json:"pkname"`
	Children    []lsblkNode `json:"children"`
}

type lsblkOutput struct {
	BlockDevices []lsblkNode `json:"blockdevices"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// includeType reports whether a device type belongs in the list (spec §8.1).
func includeType(t string, showLoop bool) bool {
	switch {
	case t == "disk" || t == "part" || t == "lvm" || t == "crypt":
		return true
	case strings.HasPrefix(t, "raid"):
		return true
	case t == "loop":
		return showLoop
	}
	return false
}

// isPseudoDevice filters zram/ram devices, which lsblk reports as disks.
func isPseudoDevice(kname string) bool {
	base := path.Base(kname)
	return strings.HasPrefix(base, "zram") || strings.HasPrefix(base, "ram")
}

// Parse converts lsblk --json output into a flat, tree-ordered device list
// (each device followed by its descendants).
func Parse(data []byte, showLoop bool) ([]Device, error) {
	var out lsblkOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("cannot parse lsblk JSON: %w", err)
	}
	var devices []Device
	// walk returns the paths the caller must record as its children: the
	// node itself when included, or — when the node is excluded (e.g. an
	// mpath map between a disk and its partitions) — the node's included
	// descendants, so ancestor/descendant safety checks never lose linkage
	// across an excluded intermediate.
	var walk func(n lsblkNode, parentPath, parentModel string) []string
	walk = func(n lsblkNode, parentPath, parentModel string) []string {
		model := deref(n.Model)
		if model == "" {
			model = parentModel // partitions inherit the disk's model
		}
		if !includeType(n.Type, showLoop) || isPseudoDevice(n.KName) {
			var promoted []string
			for _, c := range n.Children {
				promoted = append(promoted, walk(c, parentPath, model)...)
			}
			return promoted
		}
		var mounts []string
		for _, m := range n.Mountpoints {
			if m != nil && *m != "" {
				mounts = append(mounts, *m)
			}
		}
		devices = append(devices, Device{
			Path:        n.Name,
			KName:       path.Base(n.KName),
			MajMin:      n.MajMin,
			Type:        n.Type,
			SizeBytes:   int64(n.Size),
			FSType:      deref(n.FSType),
			Label:       deref(n.Label),
			UUID:        deref(n.UUID),
			PTType:      deref(n.PTType),
			Mountpoints: mounts,
			Model:       strings.TrimSpace(model),
			Serial:      strings.TrimSpace(deref(n.Serial)),
			Transport:   deref(n.Tran),
			Rotational:  bool(n.Rota),
			ReadOnly:    bool(n.RO),
			Removable:   bool(n.RM),
			Parent:      parentPath,
		})
		idx := len(devices) - 1
		selfPath := n.Name
		for _, c := range n.Children {
			devices[idx].Children = append(devices[idx].Children, walk(c, selfPath, model)...)
		}
		return []string{selfPath}
	}
	for _, n := range out.BlockDevices {
		walk(n, "", "")
	}
	return devices, nil
}

// Discover runs lsblk and parses its output. Errors indicate an environment
// problem (lsblk missing or unparseable); callers exit with code 3 (spec §12).
func Discover(showLoop bool) ([]Device, error) {
	lsblk, err := exec.LookPath("lsblk")
	if err != nil {
		return nil, fmt.Errorf("lsblk not found: cmkfs requires util-linux >= 2.33")
	}
	out, err := exec.Command(lsblk, "--json", "--bytes", "--paths", "-o", lsblkColumns).Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk failed (util-linux >= 2.33 required): %w", err)
	}
	devs, err := Parse(out, showLoop)
	if err != nil {
		return nil, fmt.Errorf("%w (util-linux >= 2.33 required)", err)
	}
	return devs, nil
}
