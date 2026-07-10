package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ethanpil/cmkfs/internal/executor"
	"github.com/ethanpil/cmkfs/internal/safety"
)

// Screen 5 — execute (spec §10.3). Deliberately hard-to-hit abort path:
// first Ctrl+C only arms a 3 s window; a second Ctrl+C opens a modal that
// requires typing ABORT. Nothing is ever killed automatically.
type execState struct {
	vp          viewport.Model
	lines       []string
	started     time.Time
	elapsed     time.Duration
	spinFrame   int
	cancel      context.CancelFunc
	ch          <-chan executor.Event
	running     bool
	ctrlCArmed  time.Time
	abortPrompt bool
	abortInput  textinput.Model
	aborting    bool
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const ctrlCWindow = 3 * time.Second

func (e *execState) resize(width, height int) {
	e.vp.Width = width - 2
	h := height - 6
	if h < 1 {
		h = 1
	}
	e.vp.Height = h
}

// sanitizeLine strips control characters (backspace progress, ANSI-breaking
// bytes) so backend output can't corrupt the viewport rendering.
func sanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func (e *execState) appendLine(line string) {
	line = sanitizeLine(line)
	e.lines = append(e.lines, line)
	atBottom := e.vp.AtBottom()
	e.vp.SetContent(strings.Join(e.lines, "\n"))
	if atBottom {
		e.vp.GotoBottom()
	}
}

func (e *execState) tickUpdate(a *App) tea.Cmd {
	if e.running {
		e.elapsed = time.Since(e.started)
		e.spinFrame = (e.spinFrame + 1) % len(spinnerFrames)
	}
	if !e.ctrlCArmed.IsZero() && time.Since(e.ctrlCArmed) > ctrlCWindow {
		e.ctrlCArmed = time.Time{}
	}
	if e.running {
		return tick()
	}
	return nil
}

func (e *execState) tooSmallStatus() string {
	if e.running {
		return "A format is RUNNING and keeps running. Output is being captured.\nCtrl+C twice then type ABORT to abort; q is disabled here."
	}
	return ""
}

// startExecute wires the FinalGate into the executor and enters Screen 5.
func (a *App) startExecute() (tea.Model, tea.Cmd) {
	gate := safety.Gate{
		Sys:          a.cfg.Sys,
		Discover:     a.cfg.Discover,
		ShowLoop:     a.cfg.ShowLoop,
		MinSizeBytes: a.fs.MinSizeBytes,
		FSName:       a.fs.Name,
		ExtraArgs:    len(a.extra),
	}
	confirmed := a.report
	fp := a.fingerprint
	path := a.dev.Path
	gateFn := func() (safety.Report, bool) { return gate.FinalGate(path, confirmed, fp) }

	ctx, cancel := context.WithCancel(context.Background())
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 16

	a.exec = execState{
		started:    time.Now(),
		cancel:     cancel,
		running:    true,
		abortInput: ti,
	}
	a.exec.resize(a.width, a.height)
	a.exec.ch = a.cfg.Run(ctx, a.argv, gateFn)
	a.screen = ScreenExecute
	return a, tea.Batch(waitEv(a.exec.ch), tick())
}

func (a *App) updateExecEvent(ev executor.Event) (tea.Model, tea.Cmd) {
	e := &a.exec

	if ev.Gate != nil {
		// Gate failure: nothing was spawned; bounce back to Screen 4 with
		// the fresh report.
		e.running = false
		e.cancel()
		a.reenterConfirmFromGate(*ev.Gate)
		return a, nil
	}
	if ev.Line != "" {
		e.appendLine(ev.Line)
	}
	if ev.Err != nil {
		e.appendLine(styleDanger.Render(fmt.Sprintf("cmkfs: output error: %v", ev.Err)))
	}
	if ev.Done {
		e.running = false
		e.cancel()
		a.finishExecution(ev)
		return a, nil
	}
	return a, waitEv(e.ch)
}

func (a *App) updateExecute(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &a.exec
	key := msg.String()

	if e.abortPrompt {
		switch key {
		case "esc":
			// Execution was never paused; just dismiss.
			e.abortPrompt = false
			e.abortInput.SetValue("")
			return a, nil
		case "enter":
			if e.abortInput.Value() == "ABORT" {
				e.abortPrompt = false
				e.aborting = true
				e.cancel() // SIGTERM group, 5 s grace, SIGKILL (spec §11)
			}
			return a, nil
		default:
			ti := e.abortInput
			ti.Focus()
			ti, _ = ti.Update(msg)
			e.abortInput = ti
			return a, nil
		}
	}

	switch key {
	case "ctrl+c":
		if !e.running {
			return a, nil
		}
		if !e.ctrlCArmed.IsZero() && time.Since(e.ctrlCArmed) <= ctrlCWindow {
			e.ctrlCArmed = time.Time{}
			e.abortPrompt = true
			e.abortInput.SetValue("")
			e.abortInput.Focus()
		} else {
			e.ctrlCArmed = time.Now()
		}
		return a, nil
	case "up", "k":
		e.vp.ScrollUp(1)
	case "down", "j":
		e.vp.ScrollDown(1)
	case "pgup":
		e.vp.HalfPageUp()
	case "pgdown":
		e.vp.HalfPageDown()
	}
	return a, nil
}

func (a *App) viewExecute() string {
	e := &a.exec
	var b strings.Builder

	elapsed := e.elapsed.Truncate(time.Second)
	header := fmt.Sprintf("%s Formatting %s as %s — %s elapsed",
		spinnerFrames[e.spinFrame], a.dev.Path, a.fs.Name, elapsed)
	if e.aborting {
		header = fmt.Sprintf("%s Aborting… %s", spinnerFrames[e.spinFrame], elapsed)
	}
	b.WriteString(styleTitle.Render("cmkfs — executing") + "\n")
	b.WriteString(styleHeader.Render(header) + "\n")
	if e.elapsed > time.Hour {
		b.WriteString(styleWarn.Render("still running — if you believe this is hung, Ctrl+C twice to abort.") + "\n")
	}
	b.WriteString(e.vp.View() + "\n")

	switch {
	case e.abortPrompt:
		b.WriteString(styleDanger.Render(fmt.Sprintf(
			"The format may be hung. Killing it now will almost certainly leave %s corrupt and unusable.", a.dev.Path)) + "\n")
		fmt.Fprintf(&b, "Type %s to kill, Esc to let it continue: %s",
			styleDanger.Render("ABORT"), e.abortInput.View())
	case !e.ctrlCArmed.IsZero():
		b.WriteString(styleWarn.Render(
			"mkfs is running; interrupting can corrupt the device. Press Ctrl+C again within 3 s to open the abort prompt."))
	default:
		b.WriteString(styleHelp.Render("↑/↓ scroll · output is live · quitting is disabled while mkfs runs"))
	}
	return b.String()
}
