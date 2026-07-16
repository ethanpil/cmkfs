// Package ui implements the cmkfs TUI: one top-level bubbletea model holding
// shared state, routing messages to the active screen (spec §10).
package ui

import (
	"context"
	"fmt"
	"path"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/executor"
	"github.com/ethanpil/cmkfs/internal/safety"
	"github.com/ethanpil/cmkfs/internal/schema"
)

// Screen identifies the active screen; navigation is strictly linear with
// back-tracking (spec §10.1).
type Screen int

const (
	ScreenDeviceList Screen = iota
	ScreenFSPicker
	ScreenOptionsForm
	ScreenConfirm
	ScreenExecute
	ScreenResult
)

const minWidth, minHeight = 80, 24

// Config wires the App to the rest of the system. Every external effect is
// injectable so the Update state machine is unit-testable without a PTY.
type Config struct {
	Schemas  []schema.Schema
	Backends map[string]device.Backend // keyed by schema Binary
	Sys      safety.System
	Discover func(showLoop bool) ([]device.Device, error)
	Run      func(ctx context.Context, argv []string, gate func() (safety.Report, bool)) <-chan executor.Event
	ShowLoop bool
	// PrintMode: after Confirm, print the command instead of executing
	// (spec §12 --print).
	PrintMode bool
	// InitialDevicePath skips Screen 1 (positional device argument).
	InitialDevicePath string
	// InitialDevices, when non-nil, seeds the device list so main's startup
	// enumeration isn't repeated (lsblk forks once per launch, not twice).
	InitialDevices []device.Device
	Version        string
}

// App is the top-level model.
type App struct {
	cfg    Config
	screen Screen
	width  int
	height int

	// selections carried through the flow (spec §10.1):
	devices     []device.Device
	devErr      error
	dev         *device.Device
	fs          *schema.Schema
	values      map[string]any
	extra       []string
	report      safety.Report
	fingerprint safety.Fingerprint
	argv        []string
	display     string

	list    listState
	picker  pickerState
	form    formState
	confirm confirmState
	exec    execState
	result  resultState

	helpOverlay bool
	quitPrompt  bool

	// PrintOut is set when the user chose to print the command instead of
	// executing; main writes it to stdout after the TUI exits.
	PrintOut string
	// FatalErr is set when the app must exit with an environment error.
	FatalErr error
}

// NewApp builds the model and performs the initial device enumeration.
func NewApp(cfg Config) *App {
	if cfg.Run == nil {
		cfg.Run = executor.Run
	}
	a := &App{cfg: cfg, width: minWidth, height: minHeight}
	a.list.cursor = -1 // nothing pre-selected: cursor starts on the header row
	if cfg.InitialDevices != nil {
		a.devices = cfg.InitialDevices
		a.list.refresh(a)
	} else {
		a.refreshDevices()
	}
	if cfg.InitialDevicePath != "" {
		for i := range a.devices {
			if a.devices[i].Path == cfg.InitialDevicePath {
				d := a.devices[i]
				a.dev = &d
				break
			}
		}
		if a.dev != nil {
			a.screen = ScreenFSPicker
			a.picker = newPickerState()
		}
	}
	return a
}

func (a *App) refreshDevices() {
	devs, err := a.cfg.Discover(a.cfg.ShowLoop)
	a.devices, a.devErr = devs, err
	a.list.refresh(a)
}

// deviceReport runs the device-level checks (no filesystem picked yet, no
// probe) for Screen 1 annotations and selectability.
func (a *App) deviceReport(d device.Device) safety.Report {
	return a.cfg.Sys.Check(safety.Params{Device: d, All: a.devices})
}

// fullReport runs the authoritative pre-confirmation check set, including
// the O_EXCL probe (Screen 4 render, spec §9 point 2).
func (a *App) fullReport(d device.Device) safety.Report {
	p := safety.Params{Device: d, All: a.devices, ExtraArgs: len(a.extra), Probe: true}
	if a.fs != nil {
		p.MinSizeBytes = a.fs.MinSizeBytes
		p.FSName = a.fs.Name
	}
	return a.cfg.Sys.Check(p)
}

// versionWarning returns the soft backend-version warning for a schema, or
// "" (spec §8.3).
func versionWarning(s schema.Schema, backends map[string]device.Backend) string {
	b, ok := backends[s.Binary]
	if !ok || !b.Found() || s.MinVersion == "" {
		return ""
	}
	if b.Version == "" {
		return fmt.Sprintf("%s version could not be determined — schema tested against %s; some options may be rejected by the backend.", s.Binary, s.MinVersion)
	}
	if device.CompareVersions(b.Version, s.MinVersion) < 0 {
		return fmt.Sprintf("%s %s is older than the tested minimum %s — some options may be rejected by the backend.", s.Binary, b.Version, s.MinVersion)
	}
	return ""
}

func (a *App) Init() tea.Cmd { return nil }

// confirmTarget is the base name the user must type on Screen 4.
func (a *App) confirmTarget() string {
	if a.dev == nil {
		return ""
	}
	return path.Base(a.dev.Path)
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type execEvMsg executor.Event

func waitEv(ch <-chan executor.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return execEvMsg(ev)
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		// Propagate to layout-owning sub-models (spec §10.4); the executor
		// keeps running untouched regardless of size.
		a.exec.resize(a.width, a.height)
		return a, nil

	case tickMsg:
		if a.screen == ScreenExecute {
			return a, a.exec.tickUpdate(a)
		}
		return a, nil

	case execEvMsg:
		return a.updateExecEvent(executor.Event(msg))

	case tea.KeyMsg:
		return a.updateKey(msg)
	}
	return a, nil
}

func (a *App) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Execute screen owns all its keys (Ctrl+C flow, abort prompt); q and
	// Esc are disabled there (spec §10.2), including in the too-small view.
	if a.screen == ScreenExecute {
		return a.updateExecute(msg)
	}

	// Terminal too small: only q works (spec §10.4).
	if a.tooSmall() {
		if key == "q" || key == "f10" || key == "ctrl+c" {
			return a, tea.Quit
		}
		return a, nil
	}

	// Quit-confirmation prompt.
	if a.quitPrompt {
		switch key {
		case "y", "Y":
			return a, tea.Quit
		case "n", "N", "esc":
			a.quitPrompt = false
		}
		return a, nil
	}

	// Help overlay.
	if a.helpOverlay {
		if key == "?" || key == "esc" || key == "q" {
			a.helpOverlay = false
		}
		return a, nil
	}

	// Long-help overlay on the options form.
	if a.screen == ScreenOptionsForm && a.form.overlayOpt != "" {
		if key == "esc" || key == "q" || key == "h" {
			a.form.overlayOpt = ""
		}
		return a, nil
	}

	switch key {
	case "ctrl+c":
		return a, tea.Quit
	case "q", "f10":
		// Text inputs need the raw q; only treat it as quit where no input
		// is focused.
		if a.textInputActive() {
			break
		}
		if a.screen > ScreenFSPicker {
			a.quitPrompt = true
			return a, nil
		}
		return a, tea.Quit
	case "?":
		if !a.textInputActive() {
			a.helpOverlay = true
			return a, nil
		}
	}

	switch a.screen {
	case ScreenDeviceList:
		return a.updateDeviceList(msg)
	case ScreenFSPicker:
		return a.updateFSPicker(msg)
	case ScreenOptionsForm:
		return a.updateOptionsForm(msg)
	case ScreenConfirm:
		return a.updateConfirm(msg)
	case ScreenResult:
		return a.updateResult(msg)
	}
	return a, nil
}

// textInputActive reports whether a focused text input should swallow
// printable keys on the current screen.
func (a *App) textInputActive() bool {
	switch a.screen {
	case ScreenOptionsForm:
		return a.form.textFieldFocused(a)
	case ScreenConfirm:
		return a.confirm.typedMode
	}
	return false
}

func (a *App) tooSmall() bool {
	return a.width < minWidth || a.height < minHeight
}

func (a *App) View() string {
	if a.tooSmall() {
		msg := fmt.Sprintf("Terminal too small (need %dx%d, have %dx%d)", minWidth, minHeight, a.width, a.height)
		if a.screen == ScreenExecute {
			msg += "\n" + a.exec.tooSmallStatus()
		}
		return msg + "\nPress q to quit.\n"
	}
	if a.quitPrompt {
		return styleTitle.Render("cmkfs") + "\n\n" +
			styleWarn.Render("Quit cmkfs? Selections will be lost.") + "\n\n" +
			"  y — quit    n — stay\n"
	}
	if a.helpOverlay {
		return a.viewHelpOverlay()
	}
	switch a.screen {
	case ScreenDeviceList:
		return a.viewDeviceList()
	case ScreenFSPicker:
		return a.viewFSPicker()
	case ScreenOptionsForm:
		return a.viewOptionsForm()
	case ScreenConfirm:
		return a.viewConfirm()
	case ScreenExecute:
		return a.viewExecute()
	case ScreenResult:
		return a.viewResult()
	}
	return ""
}

func (a *App) viewHelpOverlay() string {
	rows := [][2]string{
		{"Up/Down, j/k", "Move selection"},
		{"Enter", "Select / advance"},
		{"Esc", "Back one screen (disabled during execution)"},
		{"q, F10", "Quit (confirmation past filesystem pick; disabled during execution)"},
		{"?", "Toggle this help"},
		{"r", "Refresh device list (device list only)"},
		{"h", "Extended help for the focused option (options form)"},
		{"a", "Expand Advanced — Extra Arguments (options form)"},
		{"p", "Print the command and exit instead of executing (confirm screen)"},
	}
	title := "cmkfs"
	if a.cfg.Version != "" {
		title += " " + a.cfg.Version
	}
	out := styleTitle.Render(title+" — keys") + "\n\n"
	for _, r := range rows {
		out += fmt.Sprintf("  %-14s %s\n", styleHeader.Render(r[0]), r[1])
	}
	return out + "\n" + styleHelp.Render("Press ? or Esc to close.")
}
