package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ethanpil/cmkfs/internal/executor"
	"github.com/ethanpil/cmkfs/internal/safety"
)

// Screen 6 — result (spec §10.3).
type resultState struct {
	success bool
	aborted bool
	exit    int
	uuid    string
	fstype  string
}

// finishExecution moves to the result screen; on success the device is
// re-probed via lsblk for the new filesystem's UUID.
func (a *App) finishExecution(ev executor.Event) {
	r := resultState{
		success: ev.Exit == 0 && !ev.Aborted,
		aborted: ev.Aborted,
		exit:    ev.Exit,
	}
	if r.success {
		if devs, err := a.cfg.Discover(a.cfg.ShowLoop); err == nil {
			for _, d := range devs {
				if d.Path == a.dev.Path {
					r.uuid = d.UUID
					r.fstype = d.FSType
					break
				}
			}
		}
	}
	a.result = r
	a.screen = ScreenResult
}

func (a *App) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n":
		// Format another device: back to a refreshed Screen 1.
		a.dev = nil
		a.report = safety.Report{}
		a.refreshDevices()
		a.list.cursor = -1
		a.screen = ScreenDeviceList
	case "q", "enter":
		return a, tea.Quit
	case "up", "k":
		a.exec.vp.ScrollUp(1)
	case "down", "j":
		a.exec.vp.ScrollDown(1)
	case "pgup":
		a.exec.vp.HalfPageUp()
	case "pgdown":
		a.exec.vp.HalfPageDown()
	}
	return a, nil
}

func (a *App) viewResult() string {
	var b strings.Builder
	r := a.result

	switch {
	case r.success:
		b.WriteString(styleSuccessBanner.Render(" Format complete ") + "\n\n")
		fmt.Fprintf(&b, "  Device:      %s\n", styleHeader.Render(a.dev.Path))
		fmt.Fprintf(&b, "  Filesystem:  %s\n", a.fs.Name)
		if r.uuid != "" {
			fmt.Fprintf(&b, "  UUID:        %s\n", r.uuid)
		}
		fmt.Fprintf(&b, "  Command:     %s\n", styleCommand.Render(a.display))
	case r.aborted:
		b.WriteString(styleAbortBanner.Render(" Aborted ") + "\n\n")
		b.WriteString(styleWarn.Render(fmt.Sprintf(
			"  The format of %s was killed mid-run. The device state is unknown;\n  re-format it or run wipefs -a before use.", a.dev.Path)) + "\n")
	default:
		b.WriteString(styleFailBanner.Render(fmt.Sprintf(" mkfs failed (exit %d) ", r.exit)) + "\n\n")
		fmt.Fprintf(&b, "  Command: %s\n", styleCommand.Render(a.display))
	}

	// The viewport retains the backend output for scrolling (spec §10.3).
	if len(a.exec.lines) > 0 {
		b.WriteString("\n" + a.exec.vp.View() + "\n")
	}
	b.WriteString("\n" + styleHelp.Render("n format another device · q quit"))
	return b.String()
}
