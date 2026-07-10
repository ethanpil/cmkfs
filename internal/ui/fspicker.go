package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/schema"
)

// Screen 2 — filesystem picker (spec §10.3).
type pickerState struct {
	cursor int
}

func newPickerState() pickerState { return pickerState{} }

// installHint maps a backend binary to the package that provides it.
var installHint = map[string]string{
	"mkfs.ext4":  "e2fsprogs",
	"mkfs.xfs":   "xfsprogs",
	"mkfs.btrfs": "btrfs-progs",
	"mkfs.fat":   "dosfstools",
	"mkfs.exfat": "exfatprogs",
	"mkfs.f2fs":  "f2fs-tools",
}

// pickerDisabledReason returns "" when the schema is selectable for the
// current device.
func (a *App) pickerDisabledReason(s schema.Schema) string {
	b, ok := a.cfg.Backends[s.Binary]
	if !ok || !b.Found() {
		hint := installHint[s.Binary]
		if hint == "" {
			hint = "the filesystem tools"
		}
		return fmt.Sprintf("%s not found — install %s", s.Binary, hint)
	}
	if a.dev != nil && s.MinSizeBytes > 0 && a.dev.SizeBytes < s.MinSizeBytes {
		return fmt.Sprintf("%s requires at least %s; %s is %s.", s.Name, device.HumanSize(s.MinSizeBytes), a.dev.Path, device.HumanSize(a.dev.SizeBytes))
	}
	return ""
}

func (a *App) updateFSPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.picker.cursor > 0 {
			a.picker.cursor--
		}
	case "down", "j":
		if a.picker.cursor < len(a.cfg.Schemas)-1 {
			a.picker.cursor++
		}
	case "esc":
		a.screen = ScreenDeviceList
	case "enter":
		s := a.cfg.Schemas[a.picker.cursor]
		if a.pickerDisabledReason(s) != "" {
			break
		}
		fs := s
		samefs := a.fs != nil && a.fs.ID == fs.ID
		a.fs = &fs
		if !samefs {
			a.form = newFormState(a) // re-picking the same fs keeps form values
		}
		a.screen = ScreenOptionsForm
	}
	return a, nil
}

func (a *App) viewFSPicker() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("cmkfs — choose a filesystem") + "\n\n")
	if a.dev != nil {
		fmt.Fprintf(&b, "Target: %s (%s)\n\n", styleHeader.Render(a.dev.Path), device.HumanSizeCompact(a.dev.SizeBytes))
	}
	// The renderer truncates at the window width, so descriptions and
	// reasons are wrapped with a hanging indent under the name column. The
	// name column is sized to the widest schema Name so it never overflows
	// when a longer filesystem is added.
	nameW := 0
	for _, s := range a.cfg.Schemas {
		if w := lipgloss.Width(s.Name); w > nameW {
			nameW = w
		}
	}
	nameCol := nameW + 3 // "  " + name column + " "
	indent := strings.Repeat(" ", nameCol)
	for i, s := range a.cfg.Schemas {
		reason := a.pickerDisabledReason(s)
		pad := strings.Repeat(" ", nameW-lipgloss.Width(s.Name))
		desc := wrapIndent(s.Description, a.width-nameCol-1, indent)
		line := "  " + s.Name + pad + " " + desc
		var reasonLine string
		if reason != "" {
			reasonLine = indent + wrapIndent(reason, a.width-nameCol-1, indent)
		}
		switch {
		case i == a.picker.cursor && reason == "":
			b.WriteString(styleSelected.Render(line) + "\n")
		case i == a.picker.cursor:
			b.WriteString(styleSelected.Render(line) + "\n")
			b.WriteString(styleDim.Render(reasonLine) + "\n")
		case reason != "":
			b.WriteString(styleDim.Render(line) + "\n")
			b.WriteString(styleDim.Render(reasonLine) + "\n")
		default:
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n")
	// Persistent soft version warnings (spec §8.3).
	for _, s := range a.cfg.Schemas {
		if w := versionWarning(s, a.cfg.Backends); w != "" {
			b.WriteString(styleWarn.Render(wordWrap(w, a.width-2)) + "\n")
		}
	}
	b.WriteString(styleHelp.Render("↑/↓ move · Enter select · Esc back · ? keys · q quit"))
	return b.String()
}
