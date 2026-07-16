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

	// Every column takes exactly the width its content needs, so short paths
	// leave more room for MODEL. When that still overflows the window, MODEL
	// and then LABEL shrink to their header widths with an ellipsis implying
	// there is more; the whole-row clamp is the backstop for anything else.
	const colLabel, colModel = 4, 6
	headerCells := [7]string{"PATH", "SIZE", "TYPE", "FSTYPE", "LABEL", "MOUNTPOINT", "MODEL"}
	rows := make([][7]string, len(a.devices))
	for i, d := range a.devices {
		mount := ""
		if len(d.Mountpoints) > 0 {
			mount = d.Mountpoints[0]
			if len(d.Mountpoints) > 1 {
				mount += fmt.Sprintf(" (+%d)", len(d.Mountpoints)-1)
			}
		}
		indent := strings.Repeat("  ", a.list.depths[i])
		rows[i] = [7]string{indent + d.Path, device.HumanSizeCompact(d.SizeBytes),
			d.Type, d.FSType, d.Label, mount, d.Model}
	}
	var w [7]int
	for c, h := range headerCells {
		w[c] = len(h)
	}
	for _, r := range rows {
		for c, cell := range r {
			if n := len([]rune(cell)); n > w[c] {
				w[c] = n
			}
		}
	}
	total := func() int {
		t := 2 + 2*6 // leading indent + six two-space gaps
		for _, x := range w {
			t += x
		}
		return t
	}
	for _, c := range []int{colModel, colLabel} {
		if over := total() - a.width; over > 0 {
			w[c] = max(w[c]-over, len(headerCells[c]))
		}
	}
	line := func(cells [7]string) string {
		cells[colLabel] = trunc(cells[colLabel], w[colLabel])
		cells[colModel] = trunc(cells[colModel], w[colModel])
		return trunc(fmt.Sprintf("  %-*s  %*s  %-*s  %-*s  %-*s  %-*s  %s",
			w[0], cells[0], w[1], cells[1], w[2], cells[2], w[3], cells[3],
			w[4], cells[4], w[5], cells[5], cells[6]), a.width)
	}

	header := line(headerCells)
	if a.list.cursor == -1 {
		b.WriteString(styleSelected.Render(header) + "\n")
	} else {
		b.WriteString(styleHeader.Render(header) + "\n")
	}

	if len(a.devices) == 0 {
		b.WriteString(styleDim.Render("  (no block devices found)") + "\n")
	}

	for i := range rows {
		row := line(rows[i])
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
