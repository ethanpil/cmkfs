package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ethanpil/cmkfs/internal/device"
	"github.com/ethanpil/cmkfs/internal/executor"
	"github.com/ethanpil/cmkfs/internal/safety"
	"github.com/ethanpil/cmkfs/internal/schema"
)

// testConfig wires the App to fakes: in-memory devices, empty proc/sys roots
// (no mounts, swaps, or holders), and an executor stub the test controls.
func testConfig(t *testing.T, devs []device.Device, runCh chan executor.Event) Config {
	t.Helper()
	tmp := t.TempDir()
	backends := map[string]device.Backend{}
	for _, s := range schema.Schemas {
		backends[s.Binary] = device.Backend{Binary: s.Binary, Path: "/sbin/" + s.Binary, Version: "99.0"}
	}
	return Config{
		Schemas:  schema.Schemas,
		Backends: backends,
		Sys:      safety.System{ProcRoot: tmp, SysRoot: tmp},
		Discover: func(showLoop bool) ([]device.Device, error) { return devs, nil },
		Run: func(ctx context.Context, argv []string, gate func() (safety.Report, bool)) <-chan executor.Event {
			if runCh == nil {
				ch := make(chan executor.Event)
				close(ch)
				return ch
			}
			return runCh
		},
	}
}

func cleanDisk() device.Device {
	return device.Device{Path: "/dev/sde", KName: "sde", MajMin: "8:64", Type: "disk", SizeBytes: 500107862016}
}

func signedPart() device.Device {
	return device.Device{Path: "/dev/sdf1", KName: "sdf1", MajMin: "8:81", Type: "part", SizeBytes: 4000785982464,
		FSType: "xfs", Label: "backup", UUID: "5a4b3c2d-3333-4c7d-9e5f-0123456789ab"}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(a *App, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = a.Update(key(k))
	}
	return cmd
}

func typeText(a *App, s string) {
	for _, r := range s {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// focusOption moves the form focus to the option with the given ID.
func focusOption(t *testing.T, a *App, id string) {
	t.Helper()
	for i, o := range a.fs.Options {
		if o.ID == id {
			a.form.focus = i
			return
		}
	}
	t.Fatalf("option %s not found", id)
}

func TestScreenTransitionsHappyPath(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	if a.screen != ScreenDeviceList {
		t.Fatalf("start screen = %v", a.screen)
	}
	// Cursor starts on the header row; Enter there selects nothing.
	if a.list.cursor != -1 {
		t.Fatal("cursor must start on the header row")
	}
	press(a, "enter")
	if a.screen != ScreenDeviceList {
		t.Fatal("Enter on header row must not advance")
	}
	press(a, "down", "enter")
	if a.screen != ScreenFSPicker || a.dev == nil || a.dev.Path != "/dev/sde" {
		t.Fatalf("device selection failed: screen %v dev %+v", a.screen, a.dev)
	}
	press(a, "enter") // pick ext4
	if a.screen != ScreenOptionsForm || a.fs.ID != "ext4" {
		t.Fatalf("fs pick failed: screen %v", a.screen)
	}
	press(a, "tab", "enter") // continue with defaults
	if a.screen != ScreenConfirm {
		t.Fatalf("continue failed: screen %v (footer %q)", a.screen, a.form.footerErr)
	}
	if a.display != "mkfs.ext4 /dev/sde" {
		t.Fatalf("display = %q", a.display)
	}
	// Clean device: no force flag, Yes/No selector, default No.
	if a.report.NeedsForce() {
		t.Fatal("clean device must not need force")
	}
	if a.confirm.typedMode {
		t.Fatal("no warnings -> Yes/No selector, not typed confirmation")
	}
	if a.confirm.yesNo != 0 {
		t.Fatal("selector must default to No")
	}
}

func TestEscBacktrackingPreservesFormValues(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	press(a, "down", "enter", "enter") // to ext4 form
	focusOption(t, a, "label")
	typeText(a, "media")
	if a.values["label"] != "media" {
		t.Fatalf("label value = %v", a.values["label"])
	}
	press(a, "tab", "enter")
	if a.screen != ScreenConfirm {
		t.Fatalf("expected confirm, got %v (footer %q)", a.screen, a.form.footerErr)
	}
	press(a, "esc")
	if a.screen != ScreenOptionsForm {
		t.Fatal("Esc from Confirm must return to the form")
	}
	if a.values["label"] != "media" {
		t.Fatalf("form values must survive Esc from Confirm, got %v", a.values["label"])
	}
	// Esc back to the picker and re-picking the same fs keeps values too.
	press(a, "esc", "enter")
	if a.values["label"] != "media" {
		t.Fatal("re-picking the same filesystem must keep form values")
	}
}

func TestTypedConfirmationGate(t *testing.T) {
	cfg := testConfig(t, []device.Device{signedPart()}, nil)
	cfg.PrintMode = true
	a := NewApp(cfg)
	press(a, "down", "enter", "enter", "tab", "enter")
	if a.screen != ScreenConfirm {
		t.Fatalf("expected confirm, got %v", a.screen)
	}
	if !a.confirm.typedMode {
		t.Fatal("SIGNATURE_FS warning must force typed confirmation")
	}
	if !a.report.NeedsForce() {
		t.Fatal("existing signature must set NeedsForce")
	}
	// Force flag present in the command (acceptance criterion 5).
	if !strings.Contains(a.display, "mkfs.ext4 -F ") {
		t.Fatalf("force flag missing from %q", a.display)
	}

	// Wrong name: Enter is ignored.
	typeText(a, "sdz9")
	press(a, "enter")
	if a.screen != ScreenConfirm || a.PrintOut != "" {
		t.Fatal("wrong device name must not confirm")
	}
	// Clear and type the exact base name.
	for range "sdz9" {
		press(a, "backspace")
	}
	typeText(a, "sdf1")
	press(a, "enter")
	if a.PrintOut == "" {
		t.Fatal("correct name in PrintMode must set PrintOut")
	}
	if a.PrintOut != a.display {
		t.Fatalf("PrintOut %q != display %q", a.PrintOut, a.display)
	}
}

func TestConflictDimmingAndReset(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	press(a, "down", "enter", "enter") // ext4 form

	// Set bytes_per_inode first.
	focusOption(t, a, "bytes_per_inode")
	typeText(a, "4096")
	if a.values["bytes_per_inode"] != int64(4096) {
		t.Fatalf("bytes_per_inode = %v", a.values["bytes_per_inode"])
	}
	if a.disabledReason("usage_type") == "" {
		t.Fatal("usage_type must be disabled while bytes_per_inode is set")
	}

	// Now clear it and set usage_type instead.
	for range "4096" {
		press(a, "backspace")
	}
	if a.disabledReason("usage_type") != "" {
		t.Fatal("usage_type must re-enable when the conflict clears")
	}
	focusOption(t, a, "usage_type")
	press(a, "right") // auto -> small
	if a.values["usage_type"] != "small" {
		t.Fatalf("usage_type = %v", a.values["usage_type"])
	}
	if a.disabledReason("bytes_per_inode") == "" {
		t.Fatal("bytes_per_inode must be disabled while usage_type is set")
	}
	if a.disabledReason("inode_size") == "" {
		t.Fatal("inode_size must be disabled while usage_type is set")
	}
	// And the conflicting option was reset to its omit value.
	if a.values["bytes_per_inode"] != int64(0) {
		t.Fatalf("bytes_per_inode must reset, got %v", a.values["bytes_per_inode"])
	}
}

func TestInvalidFieldBlocksContinue(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	press(a, "down", "enter", "enter")
	focusOption(t, a, "label")
	typeText(a, strings.Repeat("x", 17)) // over the 16-byte cap
	if a.form.invalid["label"] == "" {
		t.Fatal("oversized label must be flagged invalid")
	}
	press(a, "tab", "enter")
	if a.screen != ScreenOptionsForm {
		t.Fatal("invalid field must block Continue")
	}
	if a.form.footerErr == "" {
		t.Fatal("footer must show the validation error")
	}
}

func TestExtraArgsListAndGuardrails(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	press(a, "down", "enter", "enter")
	focusOption(t, a, "journal") // a is literal input on text fields
	press(a, "a")                // expand Advanced
	if !a.form.advancedOpen {
		t.Fatal("a must expand the Advanced section")
	}
	// Focus the extra input (after the last option).
	a.form.focus = len(a.fs.Options)
	typeText(a, "-E")
	press(a, "enter")
	typeText(a, "nodiscard")
	press(a, "enter")
	if len(a.extra) != 2 || a.extra[0] != "-E" || a.extra[1] != "nodiscard" {
		t.Fatalf("extra = %v", a.extra)
	}
	// Guardrail: /dev/ path rejected at add time with the reason shown.
	typeText(a, "/dev/sdz")
	press(a, "enter")
	if len(a.extra) != 2 {
		t.Fatalf("guardrail failed: %v", a.extra)
	}
	if !strings.Contains(a.form.extraErr, "/dev/") {
		t.Fatalf("extraErr = %q", a.form.extraErr)
	}

	// Continue: EXTRA_ARGS warning must force typed confirmation.
	press(a, "tab", "enter")
	if a.screen != ScreenConfirm {
		t.Fatalf("continue failed: %v %q", a.screen, a.form.footerErr)
	}
	if !a.report.Has("EXTRA_ARGS") {
		t.Fatal("EXTRA_ARGS finding missing")
	}
	if !a.confirm.typedMode {
		t.Fatal("extra args must force typed confirmation")
	}
	if a.report.NeedsForce() {
		t.Fatal("extra args alone must never inject force")
	}
	// Tokens appear before the device in the command.
	if !strings.Contains(a.display, "-E nodiscard /dev/sde") {
		t.Fatalf("display = %q", a.display)
	}
}

func TestBlockedDeviceUnselectable(t *testing.T) {
	ro := cleanDisk()
	ro.ReadOnly = true
	a := NewApp(testConfig(t, []device.Device{ro}, nil))
	press(a, "down", "enter")
	if a.screen != ScreenDeviceList {
		t.Fatal("blocked (read-only) device must not be selectable")
	}
}

func TestGateFailureBouncesToConfirm(t *testing.T) {
	runCh := make(chan executor.Event, 1)
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, runCh))
	press(a, "down", "enter", "enter", "tab", "enter")
	press(a, "right", "enter") // Yes
	if a.screen != ScreenExecute {
		t.Fatalf("expected execute, got %v", a.screen)
	}
	gateReport := safety.Report{Findings: []safety.Finding{
		{Severity: safety.Blocker, Code: "MOUNTED", Message: "/dev/sde is mounted at /mnt. Unmount it first."},
	}}
	a.Update(execEvMsg(executor.Event{Done: true, Exit: -1, Gate: &gateReport}))
	if a.screen != ScreenConfirm {
		t.Fatalf("gate failure must bounce to Confirm, got %v", a.screen)
	}
	if !a.report.Has("MOUNTED") {
		t.Fatal("fresh gate report must be shown")
	}
	if !a.report.Blocked() {
		t.Fatal("report must be blocked")
	}
}

func TestExecuteFlowAndResult(t *testing.T) {
	runCh := make(chan executor.Event, 8)
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, runCh))
	press(a, "down", "enter", "enter", "tab", "enter", "right", "enter")
	if a.screen != ScreenExecute {
		t.Fatalf("expected execute, got %v", a.screen)
	}
	a.Update(execEvMsg(executor.Event{Line: "Creating filesystem..."}))
	a.Update(execEvMsg(executor.Event{Line: "done"}))
	if len(a.exec.lines) != 2 {
		t.Fatalf("lines = %v", a.exec.lines)
	}
	a.Update(execEvMsg(executor.Event{Done: true, Exit: 0}))
	if a.screen != ScreenResult || !a.result.success {
		t.Fatalf("expected success result, got %v %+v", a.screen, a.result)
	}
	// n goes back to a refreshed device list.
	press(a, "n")
	if a.screen != ScreenDeviceList || a.dev != nil {
		t.Fatal("n must return to the device list")
	}
}

func TestExecuteAbortFlow(t *testing.T) {
	runCh := make(chan executor.Event, 8)
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, runCh))
	press(a, "down", "enter", "enter", "tab", "enter", "right", "enter")

	// A single Ctrl+C never kills anything: it only arms the window.
	press(a, "ctrl+c")
	if a.exec.abortPrompt {
		t.Fatal("single Ctrl+C must not open the abort prompt")
	}
	if a.exec.ctrlCArmed.IsZero() {
		t.Fatal("first Ctrl+C must arm the window")
	}
	press(a, "ctrl+c")
	if !a.exec.abortPrompt {
		t.Fatal("second Ctrl+C within the window must open the abort prompt")
	}
	// Esc dismisses; execution was never paused.
	press(a, "esc")
	if a.exec.abortPrompt {
		t.Fatal("Esc must dismiss the abort prompt")
	}
	// Reopen and type the wrong word: nothing happens.
	press(a, "ctrl+c", "ctrl+c")
	typeText(a, "abort")
	press(a, "enter")
	if a.exec.aborting {
		t.Fatal("lowercase abort must not kill")
	}
	for range "abort" {
		press(a, "backspace")
	}
	typeText(a, "ABORT")
	press(a, "enter")
	if !a.exec.aborting {
		t.Fatal("typed ABORT must cancel the executor")
	}
	// Executor reports the aborted death.
	a.Update(execEvMsg(executor.Event{Done: true, Exit: -1, Aborted: true}))
	if a.screen != ScreenResult || !a.result.aborted {
		t.Fatalf("expected aborted result, got %v %+v", a.screen, a.result)
	}
}

func TestQuitDisabledDuringExecute(t *testing.T) {
	runCh := make(chan executor.Event, 1)
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, runCh))
	press(a, "down", "enter", "enter", "tab", "enter", "right", "enter")
	if cmd := press(a, "q"); cmd != nil {
		t.Fatal("q must be disabled during execution")
	}
	if cmd := press(a, "esc"); cmd != nil {
		t.Fatal("esc must be disabled during execution")
	}
	if a.screen != ScreenExecute {
		t.Fatal("must stay on execute screen")
	}
}

func TestTerminalTooSmall(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	view := a.View()
	if !strings.Contains(view, "Terminal too small (need 80x24, have 60x20)") {
		t.Fatalf("view = %q", view)
	}
	// Growing back restores the UI.
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if strings.Contains(a.View(), "Terminal too small") {
		t.Fatal("view must restore after resize")
	}
}

func TestInitialDeviceSkipsScreen1(t *testing.T) {
	cfg := testConfig(t, []device.Device{cleanDisk()}, nil)
	cfg.InitialDevicePath = "/dev/sde"
	a := NewApp(cfg)
	if a.screen != ScreenFSPicker || a.dev == nil || a.dev.Path != "/dev/sde" {
		t.Fatalf("positional device must start on Screen 2: %v", a.screen)
	}
}

func TestPickerDisabledWhenBackendMissing(t *testing.T) {
	cfg := testConfig(t, []device.Device{cleanDisk()}, nil)
	cfg.Backends["mkfs.xfs"] = device.Backend{Binary: "mkfs.xfs"} // not found
	a := NewApp(cfg)
	press(a, "down", "enter")
	// Move to xfs (second entry) and try to select it.
	press(a, "down", "enter")
	if a.screen != ScreenFSPicker {
		t.Fatal("missing backend must not be selectable")
	}
	if !strings.Contains(a.View(), "mkfs.xfs not found — install xfsprogs") {
		t.Fatal("missing-backend reason must be shown")
	}
}

func TestPickerTooSmallDevice(t *testing.T) {
	small := cleanDisk()
	small.SizeBytes = 134217728 // 128 MiB < xfs 300 MiB minimum
	a := NewApp(testConfig(t, []device.Device{small}, nil))
	press(a, "down", "enter")
	press(a, "down", "enter") // try xfs
	if a.screen != ScreenFSPicker {
		t.Fatal("too-small device must not allow xfs")
	}
	// ext4 (no minimum) still works.
	press(a, "up", "enter")
	if a.screen != ScreenOptionsForm || a.fs.ID != "ext4" {
		t.Fatalf("ext4 must remain selectable, got %v", a.screen)
	}
}

func TestVersionWarningShown(t *testing.T) {
	cfg := testConfig(t, []device.Device{cleanDisk()}, nil)
	cfg.Backends["mkfs.xfs"] = device.Backend{Binary: "mkfs.xfs", Path: "/sbin/mkfs.xfs", Version: "4.5.0"}
	a := NewApp(cfg)
	press(a, "down", "enter")
	if !strings.Contains(a.View(), "mkfs.xfs 4.5.0 is older than the tested minimum 5.0.0") {
		t.Fatalf("version warning missing from picker view")
	}
}

// TestAdvancedCollapseClampsFocus: opening Advanced, tabbing to Continue,
// then collapsing Advanced must not leave the focus index past the end of
// the shrunken target list (regression: render panic).
func TestAdvancedCollapseClampsFocus(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	press(a, "down", "enter", "enter")
	focusOption(t, a, "journal") // non-text row so a is a hotkey
	press(a, "a", "tab")         // open Advanced, jump to Continue (last target)
	press(a, "up", "a")          // move onto a target that exists only while open, collapse
	// Must not panic and must stay renderable.
	_ = a.View()
	if a.form.focus >= len(a.formTargets()) {
		t.Fatalf("focus %d out of range of %d targets", a.form.focus, len(a.formTargets()))
	}
	// The original crash sequence: collapse while ON the last target.
	press(a, "a", "tab", "a")
	_ = a.View()
}

// TestDeviceListSurvivesGrowthDuringFlow: a device appearing between Screen 1
// and Confirm must not desync the list's parallel slices (regression: panic
// on Esc back to the device list).
func TestDeviceListSurvivesGrowthDuringFlow(t *testing.T) {
	devs := []device.Device{cleanDisk()}
	cfg := testConfig(t, devs, nil)
	grown := false
	cfg.Discover = func(showLoop bool) ([]device.Device, error) {
		if grown {
			extra := cleanDisk()
			extra.Path, extra.KName, extra.MajMin = "/dev/sdz", "sdz", "8:240"
			return []device.Device{cleanDisk(), extra}, nil
		}
		return devs, nil
	}
	a := NewApp(cfg)
	press(a, "down", "enter", "enter")
	grown = true // a USB stick is plugged in while the form is open
	press(a, "tab", "enter")
	if a.screen != ScreenConfirm {
		t.Fatalf("expected confirm, got %v", a.screen)
	}
	press(a, "esc", "esc", "esc") // back to the device list
	if a.screen != ScreenDeviceList {
		t.Fatalf("expected device list, got %v", a.screen)
	}
	_ = a.View() // must not panic on the grown list
	press(a, "down", "down")
	_ = a.View()
}

// TestGateBounceRebuildsCommand: after a gate failure the confirm screen must
// show a command matching the fresh report's force decision.
func TestGateBounceRebuildsCommand(t *testing.T) {
	runCh := make(chan executor.Event, 1)
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, runCh))
	press(a, "down", "enter", "enter", "tab", "enter", "right", "enter")
	if a.screen != ScreenExecute {
		t.Fatalf("expected execute, got %v", a.screen)
	}
	if strings.Contains(a.display, "-F") {
		t.Fatalf("clean device must not have force: %q", a.display)
	}
	// Gate discovers a signature appeared: warning-only fresh report.
	gateReport := safety.Report{Findings: []safety.Finding{
		{Severity: safety.Warning, Code: "SIGNATURE_FS", Message: "Existing xfs filesystem will be destroyed."},
	}}
	a.Update(execEvMsg(executor.Event{Done: true, Exit: -1, Gate: &gateReport}))
	if a.screen != ScreenConfirm {
		t.Fatalf("expected confirm, got %v", a.screen)
	}
	if !strings.Contains(a.display, "mkfs.ext4 -F ") {
		t.Fatalf("rebuilt command must include force after signature warning: %q", a.display)
	}
	if !a.confirm.typedMode {
		t.Fatal("fresh warning must require typed confirmation")
	}
}

// TestResultNewRunResetsSelections: 'n' on the result screen must not carry
// the previous run's label/options/extra args to the next device.
func TestResultNewRunResetsSelections(t *testing.T) {
	runCh := make(chan executor.Event, 4)
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, runCh))
	press(a, "down", "enter", "enter")
	focusOption(t, a, "label")
	typeText(a, "oldlabel")
	focusOption(t, a, "journal")
	press(a, "a")
	a.form.focus = len(a.fs.Options)
	typeText(a, "-E")
	press(a, "enter")
	press(a, "tab", "enter") // continue
	typeText(a, "sde")       // extra arg forces typed confirmation
	press(a, "enter")
	a.Update(execEvMsg(executor.Event{Done: true, Exit: 0}))
	if a.screen != ScreenResult {
		t.Fatalf("expected result, got %v", a.screen)
	}
	press(a, "n")
	if a.fs != nil || a.values != nil || a.extra != nil {
		t.Fatalf("selections must reset: fs=%v values=%v extra=%v", a.fs, a.values, a.extra)
	}
	// A fresh run starts with defaults.
	press(a, "down", "enter", "enter")
	if a.values["label"] != "" {
		t.Fatalf("label must be back to default, got %v", a.values["label"])
	}
	if len(a.extra) != 0 {
		t.Fatalf("extra args must be cleared, got %v", a.extra)
	}
}

// TestViewsFitWidth: the renderer truncates lines wider than the terminal,
// so the text-heavy interactive screens (picker, options form and its
// overlays, confirm) must wrap their content to the window width — the
// picker descriptions and soft version warnings once overflowed 80-column
// consoles. The columnar device list is a separate, table-layout concern.
func TestViewsFitWidth(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	a.width, a.height = 80, 24
	// Exercise the wrapped install-hint reason and a soft version warning.
	delete(a.cfg.Backends, "mkfs.exfat")
	a.cfg.Backends["mkfs.fat"] = device.Backend{Binary: "mkfs.fat", Path: "/sbin/mkfs.fat"}

	checkFit := func(context string) {
		t.Helper()
		for _, line := range strings.Split(a.View(), "\n") {
			if w := lipgloss.Width(line); w > a.width {
				t.Errorf("%s: line is %d columns wide, window is %d:\n%q", context, w, a.width, line)
			}
		}
	}

	press(a, "down", "enter") // select the device, land on the picker
	for i, s := range a.cfg.Schemas {
		a.picker.cursor = i
		checkFit("picker on " + s.ID)
	}

	longLabel := strings.Repeat("x", 120) // a valid-but-long typed value

	for _, s := range a.cfg.Schemas {
		fs := s
		a.fs = &fs
		a.form = newFormState(a)
		a.screen = ScreenOptionsForm
		for i := range a.formTargets() {
			a.form.focus = i
			checkFit(s.ID + " form")
		}
		// A long typed value must scroll inside its field, not overrun.
		if ti, ok := a.form.inputs["label"]; ok {
			ti.SetValue(longLabel)
			a.form.inputs["label"] = ti
			checkFit(s.ID + " form long label (blurred)")
			focusOption(t, a, "label")
			checkFit(s.ID + " form long label (focused)")
		}
		// A long extra-argument token must wrap under its gutter.
		a.form.advancedOpen = true
		a.extra = []string{longLabel}
		a.form.extraErr = strings.Repeat("e", 120)
		checkFit(s.ID + " form advanced")
		a.extra = nil
		a.form.extraErr = ""
		a.form.advancedOpen = false
		for _, o := range fs.Options {
			a.form.overlayOpt = o.ID
			checkFit(s.ID + " overlay " + o.ID)
		}
		a.form.overlayOpt = ""
	}

	// Confirm screen: a long finding message and a long device model.
	longDev := cleanDisk()
	longDev.Model = strings.Repeat("M", 90)
	a.dev = &longDev
	a.report = safety.Report{Findings: []safety.Finding{
		{Severity: safety.Warning, Code: "SIGNATURE_FS", Message: "Existing xfs filesystem " + strings.Repeat("z", 90) + " will be destroyed."},
	}}
	fs := a.cfg.Schemas[0]
	a.fs = &fs
	a.argv = []string{"mkfs.ext4", longDev.Path}
	a.display = "mkfs.ext4 " + longDev.Path
	a.confirm = confirmState{typedMode: true}
	a.screen = ScreenConfirm
	checkFit("confirm long finding + model")

	// Device list: an over-long device-mapper path, a long model, and a long
	// safety finding must all stay within the window.
	wide := cleanDisk()
	wide.Path = "/dev/mapper/" + strings.Repeat("x", 44)
	wide.Model = strings.Repeat("M", 60)
	a.devices = []device.Device{wide}
	a.list.refresh(a) // recomputes reports; overwrite the focused one below
	a.list.reports[0] = safety.Report{Findings: []safety.Finding{
		{Severity: safety.Warning, Code: "SIGNATURE_FS", Message: "Existing ext4 filesystem " + strings.Repeat("z", 90) + " will be destroyed."},
	}}
	a.list.cursor = 0
	a.screen = ScreenDeviceList
	checkFit("device list long row + finding")
}

// TestDeviceListFooterOrder: the key-hint line sits directly under the table
// and stays put; the focused device's findings render below it.
func TestDeviceListFooterOrder(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{signedPart()}, nil))
	press(a, "down") // focus the device: SIGNATURE_FS finding appears
	view := a.View()
	hints := strings.Index(view, "↑/↓ move")
	finding := strings.Index(view, "Existing xfs filesystem")
	if hints == -1 || finding == -1 {
		t.Fatalf("hints or finding missing from view:\n%s", view)
	}
	if hints > finding {
		t.Fatal("key hints must render above the findings")
	}
	// The hint line must not move when the focus changes (header row has no
	// findings block at all).
	row := strings.Count(view[:hints], "\n")
	press(a, "up")
	view = a.View()
	hints = strings.Index(view, "↑/↓ move")
	if hints == -1 || strings.Count(view[:hints], "\n") != row {
		t.Fatalf("hint line moved between focus states:\n%s", view)
	}
}

func TestBoolToggleEmission(t *testing.T) {
	a := NewApp(testConfig(t, []device.Device{cleanDisk()}, nil))
	press(a, "down", "enter", "enter")
	focusOption(t, a, "journal")
	press(a, "space")
	if a.values["journal"] != false {
		t.Fatalf("journal = %v", a.values["journal"])
	}
	press(a, "c")
	if a.display != "mkfs.ext4 -O '^has_journal' /dev/sde" {
		t.Fatalf("display = %q", a.display)
	}
}
