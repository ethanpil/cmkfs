package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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

	header := fmt.Sprintf("  %-24s %8s  %-5s %-10s %-12s %-16s %s",
		"PATH", "SIZE", "TYPE", "FSTYPE", "LABEL", "MOUNTPOINT", "MODEL")
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
		row := fmt.Sprintf("  %-24s %8s  %-5s %-10s %-12s %-16s %s",
			indent+d.Path, humanSize(d.SizeBytes), d.Type,
			trunc(d.FSType, 10), trunc(d.Label, 12), trunc(mount, 16), d.Model)
		switch {
		case i == a.list.cursor:
			b.WriteString(styleSelected.Render(row) + "\n")
		case a.list.reports[i].Blocked():
			b.WriteString(styleDim.Render(row) + "\n")
		default:
			b.WriteString(row + "\n")
		}
	}

	b.WriteString("\n")
	// Footer: focused device's findings in one line (spec §10.3).
	if a.list.cursor >= 0 && a.list.cursor < len(a.devices) {
		r := a.list.reports[a.list.cursor]
		if len(r.Findings) > 0 {
			var parts []string
			for _, f := range r.Findings {
				parts = append(parts, severityStyle(int(f.Severity)).Render(f.Message))
			}
			b.WriteString(strings.Join(parts, "  ") + "\n")
		} else {
			b.WriteString(styleSuccess.Render("No safety findings.") + "\n")
		}
	}
	b.WriteString(styleHelp.Render("↑/↓ move · Enter select · r refresh · ? keys · q quit"))
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
