package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ethanpil/cmkfs/internal/cmdgen"
	"github.com/ethanpil/cmkfs/internal/schema"
)

// Screen 3 — options form, rendered entirely from the schema (spec §10.3).
// No code here is filesystem-specific.

const (
	ftOption = iota
	ftExtraInput
	ftExtraToken
	ftContinue
)

type focusTarget struct {
	kind int
	opt  string // option ID for ftOption
	tok  int    // token index for ftExtraToken
}

type formState struct {
	schemaID     string
	focus        int
	inputs       map[string]textinput.Model // int/size/string options
	enumIdx      map[string]int
	invalid      map[string]string // option ID -> live validation error
	advancedOpen bool
	extraInput   textinput.Model
	extraErr     string
	overlayOpt   string // option ID whose long help overlay is open
	footerErr    string
}

func newFormState(a *App) formState {
	f := formState{
		schemaID: a.fs.ID,
		inputs:   map[string]textinput.Model{},
		enumIdx:  map[string]int{},
		invalid:  map[string]string{},
	}
	f.extraInput = textinput.New()
	f.extraInput.Prompt = ""
	f.extraInput.CharLimit = 256
	a.values = map[string]any{}
	a.extra = nil
	for _, o := range a.fs.Options {
		a.values[o.ID] = o.Default
		switch o.Type {
		case schema.KindEnum:
			for i, v := range o.Values {
				if v.Value == o.Default.(string) {
					f.enumIdx[o.ID] = i
				}
			}
		case schema.KindInt, schema.KindString, schema.KindSize:
			ti := textinput.New()
			ti.Prompt = ""
			ti.CharLimit = 256
			if o.MaxBytes > ti.CharLimit {
				ti.CharLimit = o.MaxBytes
			}
			ti.Placeholder = o.Placeholder
			if o.Type == schema.KindInt {
				if d := o.Default.(int64); o.Omit == nil || d != o.Omit.(int64) {
					ti.SetValue(strconv.FormatInt(d, 10))
				}
			} else if d := o.Default.(string); d != "" {
				ti.SetValue(d)
			}
			f.inputs[o.ID] = ti
		}
	}
	return f
}

// formTargets builds the focus order: options, then (when the Advanced
// section is open) the extra-args input and each token, then Continue.
func (a *App) formTargets() []focusTarget {
	var t []focusTarget
	for _, o := range a.fs.Options {
		t = append(t, focusTarget{kind: ftOption, opt: o.ID})
	}
	if a.form.advancedOpen {
		t = append(t, focusTarget{kind: ftExtraInput})
		for i := range a.extra {
			t = append(t, focusTarget{kind: ftExtraToken, tok: i})
		}
	}
	t = append(t, focusTarget{kind: ftContinue})
	return t
}

func (a *App) formTarget() focusTarget {
	targets := a.formTargets()
	if a.form.focus >= len(targets) {
		a.form.focus = len(targets) - 1
	}
	if a.form.focus < 0 {
		a.form.focus = 0
	}
	return targets[a.form.focus]
}

func (a *App) optByID(id string) schema.Option {
	for _, o := range a.fs.Options {
		if o.ID == id {
			return o
		}
	}
	return schema.Option{}
}

// disabledReason returns why an option cannot be edited right now: a set
// option conflicts with it, or its Requires are unmet (spec §10.3).
func (a *App) disabledReason(id string) string {
	o := a.optByID(id)
	for _, other := range a.fs.Options {
		if other.ID == id {
			continue
		}
		if !cmdgen.IsSet(other, a.values[other.ID]) {
			continue
		}
		for _, c := range other.Conflicts {
			if c == id {
				return fmt.Sprintf("disabled: conflicts with %s", other.Name)
			}
		}
	}
	for _, req := range o.Requires {
		if !cmdgen.IsSet(a.optByID(req), a.values[req]) {
			return fmt.Sprintf("set %s first", a.optByID(req).Name)
		}
	}
	return ""
}

// resetOption returns an option to its omit/default state (used when a
// conflicting option is set).
func (a *App) resetOption(id string) {
	o := a.optByID(id)
	a.values[id] = o.Default
	delete(a.form.invalid, id)
	switch o.Type {
	case schema.KindEnum:
		for i, v := range o.Values {
			if v.Value == o.Default.(string) {
				a.form.enumIdx[id] = i
			}
		}
	case schema.KindInt, schema.KindString, schema.KindSize:
		ti := a.form.inputs[id]
		ti.SetValue("")
		a.form.inputs[id] = ti
	}
}

// afterChange enforces conflicts: setting an option away from omit
// immediately resets every option in its Conflicts list.
func (a *App) afterChange(id string) {
	o := a.optByID(id)
	if cmdgen.IsSet(o, a.values[id]) {
		for _, c := range o.Conflicts {
			a.resetOption(c)
		}
	}
}

// syncTextValue reparses a text field into the typed values map and runs
// live validation.
func (a *App) syncTextValue(id string) {
	o := a.optByID(id)
	raw := a.form.inputs[id].Value()
	delete(a.form.invalid, id)
	switch o.Type {
	case schema.KindInt:
		if strings.TrimSpace(raw) == "" {
			if o.Omit != nil {
				a.values[id] = o.Omit
			} else {
				a.values[id] = o.Default
			}
			return
		}
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			a.form.invalid[id] = fmt.Sprintf("%s: must be a whole number", o.Name)
			return
		}
		a.values[id] = n
	case schema.KindString, schema.KindSize:
		a.values[id] = raw
	}
	if err := cmdgen.ValidateValue(*a.fs, o, a.values[id]); err != nil {
		a.form.invalid[id] = err.Error()
	}
	a.afterChange(id)
}

func (f *formState) textFieldFocused(a *App) bool {
	if a.fs == nil {
		return false
	}
	t := a.formTarget()
	switch t.kind {
	case ftExtraInput:
		return true
	case ftOption:
		o := a.optByID(t.opt)
		switch o.Type {
		case schema.KindInt, schema.KindString, schema.KindSize:
			return a.disabledReason(t.opt) == ""
		}
	}
	return false
}

func (a *App) updateOptionsForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &a.form
	f.footerErr = ""
	key := msg.String()
	t := a.formTarget()
	textFocused := f.textFieldFocused(a)

	switch key {
	case "esc":
		a.screen = ScreenFSPicker
		return a, nil
	case "up":
		if f.focus > 0 {
			f.focus--
		}
		return a, nil
	case "down":
		if f.focus < len(a.formTargets())-1 {
			f.focus++
		}
		return a, nil
	case "shift+up":
		if t.kind == ftExtraToken && t.tok > 0 {
			a.extra[t.tok-1], a.extra[t.tok] = a.extra[t.tok], a.extra[t.tok-1]
			f.focus--
		}
		return a, nil
	case "shift+down":
		if t.kind == ftExtraToken && t.tok < len(a.extra)-1 {
			a.extra[t.tok+1], a.extra[t.tok] = a.extra[t.tok], a.extra[t.tok+1]
			f.focus++
		}
		return a, nil
	case "tab":
		f.focus = len(a.formTargets()) - 1 // jump to Continue
		return a, nil
	}

	if !textFocused {
		switch key {
		case "j":
			if f.focus < len(a.formTargets())-1 {
				f.focus++
			}
			return a, nil
		case "k":
			if f.focus > 0 {
				f.focus--
			}
			return a, nil
		case "a":
			f.advancedOpen = !f.advancedOpen
			a.formTarget() // collapsing shrinks the target list; re-clamp focus
			return a, nil
		case "c":
			return a.formContinue()
		case "h":
			if t.kind == ftOption {
				f.overlayOpt = t.opt
			}
			return a, nil
		}
	}

	switch t.kind {
	case ftOption:
		o := a.optByID(t.opt)
		if a.disabledReason(t.opt) != "" {
			return a, nil
		}
		switch o.Type {
		case schema.KindBool:
			if key == " " || key == "space" || key == "enter" {
				a.values[t.opt] = !a.values[t.opt].(bool)
				a.afterChange(t.opt)
			}
		case schema.KindEnum:
			n := len(o.Values)
			switch key {
			case "left":
				f.enumIdx[t.opt] = (f.enumIdx[t.opt] + n - 1) % n
			case "right", "enter":
				f.enumIdx[t.opt] = (f.enumIdx[t.opt] + 1) % n
			default:
				return a, nil
			}
			a.values[t.opt] = o.Values[f.enumIdx[t.opt]].Value
			a.afterChange(t.opt)
		case schema.KindInt, schema.KindString, schema.KindSize:
			if key == "enter" {
				f.focus++ // advance to the next row
				return a, nil
			}
			ti := f.inputs[t.opt]
			ti.Focus()
			ti, _ = ti.Update(msg)
			f.inputs[t.opt] = ti
			a.syncTextValue(t.opt)
		}
	case ftExtraInput:
		if key == "enter" {
			tok := f.extraInput.Value()
			f.extraErr = ""
			if tok == "" {
				return a, nil
			}
			if err := cmdgen.CheckExtraToken(*a.fs, a.dev.Path, tok); err != nil {
				f.extraErr = err.Error()
				return a, nil
			}
			a.extra = append(a.extra, tok)
			f.extraInput.SetValue("")
			return a, nil
		}
		ti := f.extraInput
		ti.Focus()
		ti, _ = ti.Update(msg)
		f.extraInput = ti
	case ftExtraToken:
		switch key {
		case "d", "backspace":
			a.extra = append(a.extra[:t.tok], a.extra[t.tok+1:]...)
			if f.focus > 0 {
				f.focus--
			}
		}
	case ftContinue:
		if key == "enter" {
			return a.formContinue()
		}
	}
	return a, nil
}

// formContinue advances to Confirm only when every field validates.
func (a *App) formContinue() (tea.Model, tea.Cmd) {
	for _, msg := range a.form.invalid {
		a.form.footerErr = msg
		return a, nil
	}
	if err := cmdgen.Validate(*a.fs, a.values); err != nil {
		a.form.footerErr = err.Error()
		return a, nil
	}
	return a.enterConfirm()
}

func (a *App) viewOptionsForm() string {
	f := &a.form

	if f.overlayOpt != "" {
		return a.viewLongHelpOverlay(f.overlayOpt)
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("cmkfs — %s options for %s", a.fs.Name, a.dev.Path)) + "\n\n")

	// formTarget clamps f.focus: the target list shrinks when the Advanced
	// section collapses or tokens are removed, and rendering must never
	// index past its end.
	cur := a.formTarget()

	for _, o := range a.fs.Options {
		focused := cur.kind == ftOption && cur.opt == o.ID
		disabled := a.disabledReason(o.ID)
		invalid := f.invalid[o.ID]

		var val string
		switch o.Type {
		case schema.KindBool:
			box := "[ ]"
			if a.values[o.ID].(bool) {
				box = "[x]"
			}
			val = box
		case schema.KindEnum:
			label := o.Values[f.enumIdx[o.ID]].Label
			if focused && disabled == "" {
				val = "◄ " + label + " ►"
			} else {
				val = label
			}
		default:
			ti := f.inputs[o.ID]
			if focused && disabled == "" {
				ti.Focus()
				val = ti.View()
			} else {
				ti.Blur()
				raw := ti.Value()
				if raw == "" {
					hint := o.Placeholder
					if hint == "" {
						hint = "(backend default)"
					}
					raw = styleDim.Render(hint)
				}
				val = raw
			}
			f.inputs[o.ID] = ti
		}

		name := fmt.Sprintf("%-26s", o.Name)
		row := "  " + name + " " + val
		switch {
		case disabled != "":
			row = styleDim.Render(row + "   " + disabled)
		case invalid != "":
			row = styleInvalid.Render(row)
		case focused:
			row = styleSelected.Render("  "+name) + " " + val
		}
		b.WriteString(row + "\n")
	}

	// Advanced — Extra Arguments (spec §10.3): a collapsed section, key a.
	b.WriteString("\n")
	if !f.advancedOpen {
		label := fmt.Sprintf("  [a] Advanced — Extra arguments (%d)", len(a.extra))
		b.WriteString(styleDim.Render(label) + "\n")
	} else {
		b.WriteString(styleWarn.Render("  Extra arguments (unsupported — passed to mkfs verbatim, not validated)") + "\n")
		inputFocused := cur.kind == ftExtraInput
		ti := f.extraInput
		if inputFocused {
			ti.Focus()
		} else {
			ti.Blur()
		}
		f.extraInput = ti
		prompt := "  add token: " + ti.View()
		if inputFocused {
			prompt = styleSelected.Render("  add token:") + " " + ti.View()
		}
		b.WriteString(prompt + "\n")
		if f.extraErr != "" {
			b.WriteString("  " + styleDanger.Render(f.extraErr) + "\n")
		}
		for i, tok := range a.extra {
			row := fmt.Sprintf("    %d. %s", i+1, tok)
			if cur.kind == ftExtraToken && cur.tok == i {
				row = styleSelected.Render(row)
			} else {
				row = styleExtra.Render(row)
			}
			b.WriteString(row + "\n")
		}
		extraHelp := "Each entry is exactly one argv token: -E nodiscard is TWO tokens, -E then nodiscard.\n" +
			"Enter add · d remove · Shift+↑/↓ reorder. Routinely need a flag? Please file an issue so it can be added properly."
		b.WriteString(styleHelp.Render("  "+strings.ReplaceAll(
			wordWrap(extraHelp, a.width-4), "\n", "\n  ")) + "\n")
	}

	// Continue action.
	cont := "  [ Continue ]"
	if cur.kind == ftContinue {
		cont = styleSelected.Render(cont)
	}
	b.WriteString("\n" + cont + "\n\n")

	// Always-visible help pane for the focused option (spec §10.3).
	if cur.kind == ftOption {
		o := a.optByID(cur.opt)
		help := o.Description
		if o.Type == schema.KindEnum {
			if h := o.Values[f.enumIdx[cur.opt]].Help; h != "" {
				help += " " + h
			}
		}
		b.WriteString(styleInfo.Render(wordWrap(help, a.width-4)) + "\n")
	}

	if f.footerErr != "" {
		b.WriteString(styleDanger.Render(wordWrap(f.footerErr, a.width-2)) + "\n")
	} else if cur.kind == ftOption {
		if inv := f.invalid[cur.opt]; inv != "" {
			b.WriteString(styleDanger.Render(wordWrap(inv, a.width-2)) + "\n")
		}
	}
	b.WriteString(styleHelp.Render("↑/↓ move · h extended help · a advanced · Tab/c continue · Esc back · ? keys"))
	return b.String()
}

// viewLongHelpOverlay is the full-screen `h` overlay: long help, each enum
// value's help, and the exact flag the option maps to (spec §10.3).
func (a *App) viewLongHelpOverlay(id string) string {
	o := a.optByID(id)
	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("cmkfs — %s", o.Name)) + "\n\n")
	text := o.LongHelp
	if text == "" {
		text = o.Description
	}
	b.WriteString(wordWrap(text, a.width-4) + "\n\n")
	if o.Type == schema.KindEnum {
		b.WriteString(styleHeader.Render("Choices:") + "\n")
		labelW := 14
		for _, v := range o.Values {
			if w := lipgloss.Width(v.Label); w > labelW {
				labelW = w
			}
		}
		col := labelW + 3 // "  " + label column + " "
		for _, v := range o.Values {
			help := strings.ReplaceAll(wordWrap(v.Help, a.width-col-1),
				"\n", "\n"+strings.Repeat(" ", col))
			pad := strings.Repeat(" ", labelW-lipgloss.Width(v.Label))
			b.WriteString("  " + v.Label + pad + " " + help + "\n")
		}
		b.WriteString("\n")
	}
	flag := o.Flag
	switch {
	case o.Type == schema.KindBool:
		var parts []string
		if o.FlagTrue != "" {
			parts = append(parts, fmt.Sprintf("on: %s", o.FlagTrue))
		}
		if o.FlagFalse != "" {
			parts = append(parts, fmt.Sprintf("off: %s", o.FlagFalse))
		}
		flag = strings.Join(parts, ", ")
	case o.CompositeOnly:
		for _, c := range a.fs.Composites {
			for _, req := range c.Requires {
				if req == id {
					flag = c.Flag
				}
			}
		}
	}
	if flag != "" {
		b.WriteString(styleHeader.Render("Flag: ") + styleCommand.Render(flag) + "\n\n")
	}
	b.WriteString(styleHelp.Render("Press Esc to close."))
	return b.String()
}

func wordWrap(s string, width int) string {
	if width < 20 {
		width = 20
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		// Preserve pre-indented lines (LongHelp tables) as-is.
		if strings.HasPrefix(para, " ") {
			out = append(out, para)
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if len(line)+1+len(w) > width {
				out = append(out, line)
				line = w
			} else {
				line += " " + w
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
