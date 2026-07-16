package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/safety"
)

// Screen 1 — device list (spec §10.3). Whole disks at top level, partitions
// indented beneath. Nothing is pre-selected: the cursor starts on the header
// row (index -1) and the user must move it.
type listState struct {
	cursor  int // -1 = header row
	reports []safety.Report
	depths  []int

	// Device-information overlay (key i): the device is captured at open
	// time so a refresh underneath cannot desync it.
	infoOpen bool
	infoDev  device.Device
	details  device.Details
}

func (l *listState) refresh(a *App) {
	l.reports = make([]safety.Report, len(a.devices))
	l.depths = make([]int, len(a.devices))
	byPath := map[string]int{}
	for i, d := range a.devices {
		byPath[d.Path] = i
	}
	for i, d := range a.devices {
		l.reports[i] = a.deviceReport(d)
		depth := 0
		for p := d.Parent; p != ""; {
			pi, ok := byPath[p]
			if !ok {
				break
			}
			depth++
			p = a.devices[pi].Parent
		}
		l.depths[i] = depth
	}
	if l.cursor >= len(a.devices) {
		l.cursor = len(a.devices) - 1
	}
}

func (a *App) updateDeviceList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.list.cursor > -1 {
			a.list.cursor--
		}
	case "down", "j":
		if a.list.cursor < len(a.devices)-1 {
			a.list.cursor++
		}
	case "r":
		a.refreshDevices()
	case "i":
		if a.list.cursor >= 0 && a.list.cursor < len(a.devices) {
			a.list.infoDev = a.devices[a.list.cursor]
			a.list.details = a.cfg.Details(a.list.infoDev)
			a.list.infoOpen = true
		}
	case "enter":
		if a.list.cursor < 0 || a.list.cursor >= len(a.devices) {
			break
		}
		if a.list.reports[a.list.cursor].Blocked() {
			break // blocked devices are not selectable
		}
		d := a.devices[a.list.cursor]
		a.dev = &d
		a.screen = ScreenFSPicker
		a.picker = newPickerState()
	}
	return a, nil
}

func (a *App) viewDeviceList() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("cmkfs — select a device") + "\n\n")

	if a.devErr != nil {
		b.WriteString(styleDanger.Render(fmt.Sprintf("Device enumeration failed: %v", a.devErr)) + "\n")
		return b.String()
	}

	// lipgloss sizes each column to its content and, when the total exceeds
	// the window, takes the space back from the columns with the most slack
	// first, truncating with an ellipsis that implies more (spec §10.3).
	// Everything is measured in display cells, so wide (CJK) glyphs cannot
	// overrun the window the way a rune count would let them.
	rows := make([][]string, len(a.devices))
	for i, d := range a.devices {
		mount := ""
		if len(d.Mountpoints) > 0 {
			mount = d.Mountpoints[0]
			if len(d.Mountpoints) > 1 {
				mount += fmt.Sprintf(" (+%d)", len(d.Mountpoints)-1)
			}
		}
		rows[i] = []string{
			strings.Repeat("  ", a.list.depths[i]) + d.Path,
			device.HumanSizeCompact(d.SizeBytes), d.Type,
			d.FSType, d.Label, mount, d.Model,
		}
	}
	b.WriteString(a.deviceTable(rows) + "\n")

	if len(a.devices) == 0 {
		b.WriteString(styleDim.Render("  (no block devices found)") + "\n")
	}

	// Key hints sit directly under the table so they never move; the focused
	// device's findings render below them (spec §10.3). Nothing here emits a
	// trailing newline: the device list is the tallest screen, and a blank
	// last line costs a device row on an 80x24 terminal.
	b.WriteString("\n")
	b.WriteString(styleHelp.Render(trunc("↑/↓ move · Enter select · i info · r refresh · ? keys · q quit", a.width)))
	if a.list.cursor >= 0 && a.list.cursor < len(a.devices) {
		r := a.list.reports[a.list.cursor]
		if len(r.Findings) == 0 {
			b.WriteString("\n" + styleSuccess.Render("No safety findings."))
			return b.String()
		}
		// Findings are safety text, so wrap rather than clip: each on its own
		// line, wrapped to the window (spec §10.3).
		for _, f := range r.Findings {
			b.WriteString("\n" + severityStyle(f.Severity).Render(wordWrap(f.Message, a.width)))
		}
	}
	return b.String()
}

// deviceTable renders the device rows as a borderless table clamped to the
// window width.
func (a *App) deviceTable(rows [][]string) string {
	const colPath, colSize, colModel = 0, 1, 6
	return table.New().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).BorderBottom(false).BorderLeft(false).
		BorderRight(false).BorderColumn(false).BorderHeader(false).
		Wrap(false).
		Width(a.width).
		Headers("PATH", "SIZE", "TYPE", "FSTYPE", "LABEL", "MOUNTPOINT", "MODEL").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle()
			switch col {
			case colPath:
				s = s.PaddingLeft(2).PaddingRight(2)
			case colSize:
				s = s.Align(lipgloss.Right).PaddingRight(2)
			case colModel:
				// Last column: no trailing padding to waste.
			default:
				s = s.PaddingRight(2)
			}
			switch {
			case row == table.HeaderRow && a.list.cursor == -1:
				return s.Inherit(styleSelected)
			case row == table.HeaderRow:
				return s.Inherit(styleHeader)
			case row == a.list.cursor:
				return s.Inherit(styleSelected)
			case a.list.reports[row].Blocked():
				return s.Inherit(styleDim)
			}
			return s
		}).
		String()
}

// viewDeviceInfo renders the full-screen information overlay for the device
// captured when i was pressed. Values come from the enumeration plus the
// best-effort Details extras; unknown values render as "—".
func (a *App) viewDeviceInfo() string {
	d := a.list.infoDev
	det := a.list.details
	var b strings.Builder
	b.WriteString(styleTitle.Render("cmkfs — device information") + "\n\n")

	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}
	yesNo := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	// The key is styled, so only the plain value goes through trunc (ANSI
	// escapes would be miscounted): 2 indent + 18 key + 1 gap = 21 columns.
	row := func(k, v string) {
		b.WriteString("  " + styleHeader.Render(fmt.Sprintf("%-18s", k)) + " " + trunc(v, max(a.width-21, 1)) + "\n")
	}

	row("Path", d.Path)
	row("Kernel name", dash(d.KName))
	row("Maj:min", dash(d.MajMin))
	row("Type", dash(d.Type))
	row("Size", fmt.Sprintf("%s (%d bytes)", device.HumanSize(d.SizeBytes), d.SizeBytes))
	row("Model", dash(d.Model))
	row("Serial", dash(d.Serial))
	row("Transport", dash(d.Transport))
	row("Rotational", yesNo(d.Rotational))
	row("Removable", yesNo(d.Removable))
	row("Read-only", yesNo(d.ReadOnly))
	row("Filesystem", dash(d.FSType))
	row("Label", dash(d.Label))
	row("UUID", dash(d.UUID))
	row("Partition table", dash(d.PTType))
	row("Parent", dash(d.Parent))
	row("Children", dash(strings.Join(d.Children, ", ")))

	// One row per mountpoint, with statfs usage where it could be gathered.
	usage := map[string]device.MountUsage{}
	for _, m := range det.Mounts {
		usage[m.Mountpoint] = m
	}
	if len(d.Mountpoints) == 0 {
		row("Mounted at", "—")
	}
	for i, m := range d.Mountpoints {
		k := ""
		if i == 0 {
			k = "Mounted at"
		}
		v := m
		if u, ok := usage[m]; ok {
			v = fmt.Sprintf("%s — %s free of %s", m, device.HumanSize(u.FreeBytes), device.HumanSize(u.TotalBytes))
		}
		row(k, v)
	}

	if det.LogicalBlockSize > 0 {
		row("Block size", fmt.Sprintf("%d B logical / %d B physical", det.LogicalBlockSize, det.PhysicalBlockSize))
	}
	if det.HasTemp {
		row("Temperature", fmt.Sprintf("%.1f °C", det.TempCelsius))
	}

	b.WriteString("\n" + styleHelp.Render("Press i or Esc to close."))
	return b.String()
}

// trunc clips s to n display cells, marking the cut with an ellipsis.
// Cells, not runes: a CJK glyph occupies two columns, so a rune count would
// let a row overrun the window it was measured against.
func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}
