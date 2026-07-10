// Command gendemo drives the real cmkfs UI (internal/ui) against in-memory
// sample devices and rasterizes each screen into the animated GIF shown at
// the top of the README. It runs headlessly — no PTY, root, or real disks —
// by calling the model's Update/View directly, so the frames are the genuine
// UI output, only the device data is synthetic.
//
// Usage: go run ./internal/gendemo [out.gif]
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ethanpil/cmkfs/internal/demodata"
	"github.com/ethanpil/cmkfs/internal/ui"
)

func main() {
	// Force color output even without a TTY, so lipgloss emits the ANSI the
	// renderer colorizes. Harmless if the environment ignores it (the frame
	// then renders monochrome).
	_ = os.Setenv("CLICOLOR_FORCE", "1")
	_ = os.Setenv("COLORTERM", "truecolor")
	_ = os.Setenv("TERM", "xterm-256color")

	out := "docs/demo.gif"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	app := ui.NewApp(demodata.Config())
	app.Update(tea.WindowSizeMsg{Width: 108, Height: 26})

	type shot struct {
		text  string
		delay int // GIF delay in centiseconds
	}
	var shots []shot
	snap := func(delay int) { shots = append(shots, shot{app.View(), delay}) }

	// 1. Device list — move the cursor onto /dev/sda1 (the ext4 signature).
	press(app, key("down"), key("down"))
	snap(220)

	// 2. Filesystem picker — ext4 highlighted.
	press(app, key("enter"))
	snap(200)

	// 3. Options form — type a label so the live help pane is visible.
	press(app, key("enter"))
	typeString(app, "backups")
	snap(240)

	// 4. Confirm — the exact command (with the injected -F), the signature
	//    warning, and the typed-confirmation prompt.
	press(app, key("tab"), key("enter"))
	snap(120)
	typeString(app, "sda1")
	snap(260)

	// 5. Execute + result — accept, then pump the fake executor to completion.
	_, cmd := app.Update(key("enter"))
	execView, resultView := pump(app, cmd)
	if execView != "" {
		shots = append(shots, shot{execView, 240})
	}
	if resultView != "" {
		shots = append(shots, shot{resultView, 320})
	}

	// Rasterize. Every frame is padded to the same size (a GIF requirement).
	cols, rows := 0, 0
	grids := make([][][]cell, len(shots))
	for i, s := range shots {
		g := trimTrailingBlank(parseFrame(s.text))
		grids[i] = g
		if len(g) > rows {
			rows = len(g)
		}
		for _, line := range g {
			if len(line) > cols {
				cols = len(line)
			}
		}
	}
	if cols > maxCols {
		cols = maxCols
	}
	if rows > maxRows {
		rows = maxRows
	}

	frames := make([]*image.Paletted, len(shots))
	delays := make([]int, len(shots))
	pal := buildPalette(grids, cols, rows)
	for i := range shots {
		frames[i] = toPaletted(renderFrame(grids[i], cols, rows), pal)
		delays[i] = shots[i].delay
	}

	if dir := os.Getenv("CMKFS_DEMO_PNG"); dir != "" {
		for i := range frames {
			f, _ := os.Create(fmt.Sprintf("%s/frame%d.png", dir, i))
			_ = png.Encode(f, frames[i])
			_ = f.Close()
		}
	}

	if err := os.MkdirAll(dirOf(out), 0o755); err != nil {
		fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	if err := gif.EncodeAll(f, &gif.GIF{Image: frames, Delay: delays, LoopCount: 0}); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s: %d frames, %dx%d\n", out, len(frames), cols*cellW+2*padding, rows*cellH+2*padding)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(app *ui.App, keys ...tea.KeyMsg) {
	for _, k := range keys {
		app.Update(k)
	}
}

func typeString(app *ui.App, s string) {
	for _, r := range s {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// pump runs the executor's async commands to completion, capturing the last
// execute-screen view (most output) and the result view. tick commands are
// dropped so no wall-clock time is spent animating the spinner.
func pump(app *ui.App, cmd tea.Cmd) (execView, resultView string) {
	for steps := 0; cmd != nil && steps < 80; steps++ {
		msg := cmd()
		if msg == nil {
			break
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = nil
			for _, c := range batch {
				if c != nil {
					cmd = c // the first real command is waitEv; drop tick
					break
				}
			}
			continue
		}
		_, cmd = app.Update(msg)
		v := app.View()
		switch {
		case strings.Contains(v, "cmkfs — executing"):
			execView = v
		case strings.Contains(v, "Format complete"):
			resultView = v
		}
	}
	return execView, resultView
}

// buildPalette collects every color used across all frames (plus defaults)
// into a GIF palette, capped at 256 entries.
func buildPalette(grids [][][]cell, cols, rows int) color.Palette {
	seen := map[color.RGBA]bool{defaultBG: true, defaultFG: true}
	pal := color.Palette{defaultBG, defaultFG}
	add := func(c color.RGBA) {
		if !seen[c] && len(pal) < 256 {
			seen[c] = true
			pal = append(pal, c)
		}
	}
	for _, g := range grids {
		for ry := 0; ry < rows && ry < len(g); ry++ {
			for cx := 0; cx < cols && cx < len(g[ry]); cx++ {
				add(g[ry][cx].fg)
				add(g[ry][cx].bg)
			}
		}
	}
	return pal
}

func toPaletted(img *image.RGBA, pal color.Palette) *image.Paletted {
	p := image.NewPaletted(img.Bounds(), pal)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p.Set(x, y, img.At(x, y))
		}
	}
	return p
}

// trimTrailingBlank drops trailing rows that contain no visible glyph, so a
// short screen isn't padded to the height of the tallest one.
func trimTrailingBlank(g [][]cell) [][]cell {
	last := -1
	for i, row := range g {
		for _, c := range row {
			if c.ch > 0x20 && c.ch <= 0x7e {
				last = i
				break
			}
		}
	}
	return g[:last+1]
}

func dirOf(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[:i]
	}
	return "."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gendemo:", err)
	os.Exit(1)
}
