package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ethanpil/cmkfs/internal/cmdgen"
	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/safety"
)

// Screen 4 — confirm (spec §10.3): the exact command, a fresh safety
// re-check, and typed confirmation when any Warning exists.
type confirmState struct {
	typedMode bool // Warnings present: type the device base name
	typed     textinput.Model
	yesNo     int // 0 = No (default), 1 = Yes
}

// enterConfirm re-enumerates devices, re-runs the full check set (the report
// shown is what the user confirms against), and builds the command with
// force injected iff NeedsForce (spec §9).
func (a *App) enterConfirm() (tea.Model, tea.Cmd) {
	devs, err := a.cfg.Discover(a.cfg.ShowLoop)
	if err != nil {
		a.form.footerErr = fmt.Sprintf("cannot re-enumerate devices: %v", err)
		return a, nil
	}
	a.devices = devs
	a.list.refresh(a) // keep Screen 1's parallel report/depth slices in sync
	found := false
	for i := range devs {
		if devs[i].Path == a.dev.Path {
			d := devs[i]
			a.dev = &d
			found = true
			break
		}
	}
	if !found {
		a.report = safety.Report{Findings: []safety.Finding{{
			Severity: safety.Blocker, Code: "CHANGED_UNDER_US",
			Message: fmt.Sprintf("%s disappeared. Go back and pick another device.", a.dev.Path),
		}}}
		a.screen = ScreenConfirm
		return a, nil
	}

	a.report = a.fullReport(*a.dev)

	argv, display, err := cmdgen.Build(*a.fs, a.values, a.extra, a.dev.Path, a.report.NeedsForce(), a.report.NeedsWholeDiskFlag())
	if err != nil {
		a.form.footerErr = err.Error()
		a.screen = ScreenOptionsForm
		return a, nil
	}
	a.argv, a.display = argv, display

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 64
	ti.Focus()
	a.confirm = confirmState{
		typedMode: a.report.HasWarnings(),
		typed:     ti,
		yesNo:     0,
	}
	a.screen = ScreenConfirm
	return a, nil
}

// reenterConfirmFromGate bounces back to Screen 4 with the gate's fresh
// report; nothing was spawned (spec §10.3 Screen 5).
func (a *App) reenterConfirmFromGate(report safety.Report) {
	a.report = report
	// Refresh the device snapshot so the Pane-2 summary and the next
	// confirmation's fingerprint reflect the state the gate saw, not the
	// pre-bounce one.
	if devs, err := a.cfg.Discover(a.cfg.ShowLoop); err == nil {
		a.devices = devs
		a.list.refresh(a)
		for i := range devs {
			if devs[i].Path == a.dev.Path {
				d := devs[i]
				a.dev = &d
				break
			}
		}
	}
	// Rebuild the command: the fresh report may change the force decision,
	// and the displayed/printed command must always match the shown findings.
	if argv, display, err := cmdgen.Build(*a.fs, a.values, a.extra, a.dev.Path, report.NeedsForce(), report.NeedsWholeDiskFlag()); err == nil {
		a.argv, a.display = argv, display
	} else {
		// Unreachable with today's schemas (the same inputs already built
		// once), but never show a command that mismatches the findings.
		a.form.footerErr = err.Error()
		a.screen = ScreenOptionsForm
		return
	}
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 64
	ti.Focus()
	a.confirm = confirmState{typedMode: report.HasWarnings(), typed: ti}
	a.screen = ScreenConfirm
}

func (a *App) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "esc" {
		a.screen = ScreenOptionsForm
		return a, nil
	}

	// A Blocker appeared since Screen 1: offer only Esc/back (spec §10.3).
	if a.report.Blocked() {
		return a, nil
	}

	if key == "p" && !a.confirm.typedMode {
		a.PrintOut = a.display
		return a, tea.Quit
	}

	if a.confirm.typedMode {
		switch key {
		case "enter":
			// Enter is ignored until the typed base name matches exactly.
			if a.confirm.typed.Value() == a.confirmTarget() {
				return a.confirmAccepted()
			}
			return a, nil
		case "ctrl+p":
			a.PrintOut = a.display
			return a, tea.Quit
		default:
			ti := a.confirm.typed
			ti, _ = ti.Update(msg)
			a.confirm.typed = ti
			return a, nil
		}
	}

	// Info/no findings: Yes/No selector defaulting to No.
	switch key {
	case "left", "right", "tab":
		a.confirm.yesNo = 1 - a.confirm.yesNo
	case "y":
		a.confirm.yesNo = 1
	case "n":
		a.confirm.yesNo = 0
	case "enter":
		if a.confirm.yesNo == 1 {
			return a.confirmAccepted()
		}
		a.screen = ScreenOptionsForm
	}
	return a, nil
}

// confirmAccepted captures the fingerprint at the moment of confirmation
// (spec §9 point 2) and proceeds.
func (a *App) confirmAccepted() (tea.Model, tea.Cmd) {
	a.fingerprint = safety.FingerprintOf(*a.dev)
	if a.cfg.PrintMode {
		a.PrintOut = a.display
		return a, tea.Quit
	}
	return a.startExecute()
}

// coloredCommand renders the argv with extra-argument tokens highlighted in
// the warning color, so the unsupported portion is visually distinct.
func (a *App) coloredCommand() string {
	parts := make([]string, len(a.argv))
	extraStart := len(a.argv) - 1 - len(a.extra)
	for i, arg := range a.argv {
		q := cmdgen.ShellQuote(arg)
		if len(a.extra) > 0 && i >= extraStart && i < len(a.argv)-1 {
			parts[i] = styleExtra.Render(q)
		} else {
			parts[i] = styleCommand.Render(q)
		}
	}
	return strings.Join(parts, " ")
}

func (a *App) viewConfirm() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("cmkfs — confirm") + "\n\n")

	// Pane 1: the exact command.
	b.WriteString(styleBox.Render(a.coloredCommand()) + "\n\n")

	// Pane 2: target summary + findings from the fresh re-check.
	d := a.dev
	summary := fmt.Sprintf("%s  %s", d.Path, device.HumanSize(d.SizeBytes))
	if d.Model != "" {
		summary += "  " + d.Model
	}
	if d.Transport != "" {
		summary += "  (" + d.Transport + ")"
	}
	b.WriteString(styleHeader.Render(summary) + "\n")
	for _, f := range a.report.Findings {
		b.WriteString("  " + severityStyle(f.Severity).Render(f.Message) + "\n")
	}
	if len(a.report.Findings) == 0 {
		b.WriteString("  " + styleSuccess.Render("No safety findings.") + "\n")
	}
	if a.fs != nil {
		if w := versionWarning(*a.fs, a.cfg.Backends); w != "" {
			b.WriteString("  " + styleWarn.Render(w) + "\n")
		}
	}
	b.WriteString("\n")

	// Pane 3: confirmation.
	switch {
	case a.report.Blocked():
		b.WriteString(styleDanger.Render("Blocked. Press Esc to go back.") + "\n")
	case a.confirm.typedMode:
		fmt.Fprintf(&b, "Type the device name (%s) to confirm destruction: %s\n",
			styleDanger.Render(a.confirmTarget()), a.confirm.typed.View())
		b.WriteString(styleHelp.Render("Enter confirm (exact match required) · Esc back · Ctrl+P print command and exit"))
	default:
		yes, no := "  Yes  ", "  No  "
		if a.confirm.yesNo == 1 {
			yes = styleSelected.Render(yes)
		} else {
			no = styleSelected.Render(no)
		}
		fmt.Fprintf(&b, "Format %s? %s %s\n", a.dev.Path, no, yes)
		b.WriteString(styleHelp.Render("←/→ choose · Enter confirm · Esc back · p print command and exit"))
	}
	return b.String()
}
