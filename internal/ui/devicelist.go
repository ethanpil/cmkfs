package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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

	// The fixed columns plus an unbounded PATH/MODEL can exceed a narrow
	// window, so every row and the header are clamped to the window width
	// (the MODEL column is the first to be cut). trunc adds an ellipsis.
	header := trunc(fmt.Sprintf("  %-16s %8s  %-5s %-10s %-12s %-12s %s",
		"PATH", "SIZE", "TYPE", "FSTYPE", "LABEL", "MOUNTPOINT", "MODEL"), a.width)
	if a.list.cursor == -1 {
		b.WriteString(styleSelected.Render(header) + "\n")
	} else {
		b.WriteString(styleHeader.Render(header) + "\n")
	}

	if len(a.devices) == 0 {
		b.WriteString(styleDim.Render("  (no block devices found)") + "\n")
	}

	for i, d := range a.devices {
		indent := strings.Repeat("  ", a.list.depths[i])
		mount := ""
		if len(d.Mountpoints) > 0 {
			mount = d.Mountpoints[0]
			if len(d.Mountpoints) > 1 {
				mount += fmt.Sprintf(" (+%d)", len(d.Mountpoints)-1)
			}
		}
		row := fmt.Sprintf("  %-16s %8s  %-5s %-10s %-12s %-12s %s",
			indent+d.Path, device.HumanSizeCompact(d.SizeBytes), d.Type,
			trunc(d.FSType, 10), trunc(d.Label, 12), trunc(mount, 12), d.Model)
		row = trunc(row, a.width)
		switch {
		case i == a.list.cursor:
			b.WriteString(styleSelected.Render(row) + "\n")
		case a.list.reports[i].Blocked():
			b.WriteString(styleDim.Render(row) + "\n")
		default:
			b.WriteString(row + "\n")
		}
	}

	// Key hints sit directly under the table so they never move; the focused
	// device's findings render below them (spec §10.3).
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("↑/↓ move · Enter select · r refresh · ? keys · q quit") + "\n\n")
	if a.list.cursor >= 0 && a.list.cursor < len(a.devices) {
		r := a.list.reports[a.list.cursor]
		if len(r.Findings) > 0 {
			// Findings are safety text, so wrap rather than clip: each on its
			// own line, wrapped to the window (spec §10.3).
			for _, f := range r.Findings {
				b.WriteString(severityStyle(f.Severity).Render(wordWrap(f.Message, a.width)) + "\n")
			}
		} else {
			b.WriteString(styleSuccess.Render("No safety findings.") + "\n")
		}
	}
	return b.String()
}

func trunc(s string, n int) string {
	r := []rune(s) // slice by runes, not bytes: never split a UTF-8 sequence
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
