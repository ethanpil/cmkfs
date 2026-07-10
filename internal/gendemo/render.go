package main

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	scale   = 2 // each 8x8 glyph pixel becomes scale x scale image pixels
	cellW   = 8 * scale
	cellH   = 8 * scale
	padding = 8
	maxCols = 120
	maxRows = 30
)

var (
	defaultBG = color.RGBA{0x0d, 0x11, 0x17, 0xff} // GitHub dark canvas
	defaultFG = color.RGBA{0xc9, 0xd1, 0xd9, 0xff}
)

// cell is one terminal character with its resolved colors.
type cell struct {
	ch     byte // ASCII 0x20..0x7e; 0 = blank
	fg, bg color.RGBA
}

// substitute maps the handful of non-ASCII runes the UI emits to readable
// ASCII, so the 8x8 Basic-Latin font can render every frame.
func substitute(r rune) byte {
	switch r {
	case '─', '╭', '╮', '╰', '╯', '━':
		return '-'
	case '│', '┃':
		return '|'
	case '↑':
		return '^'
	case '↓':
		return 'v'
	case '◄', '‹', '«':
		return '<'
	case '►', '›', '»':
		return '>'
	case '·', '•':
		return '.'
	case '—', '–':
		return '-'
	case '…':
		return '.'
	case '×':
		return 'x'
	}
	if r >= 0x2800 && r <= 0x28ff { // Braille block: the spinner
		return '*'
	}
	if r >= 0x20 && r < 0x7f {
		return byte(r)
	}
	return ' '
}

// xterm256 returns the RGB of a 0..255 xterm palette index.
func xterm256(n int) color.RGBA {
	basic := []color.RGBA{
		{0, 0, 0, 255}, {205, 49, 49, 255}, {13, 188, 121, 255}, {229, 229, 16, 255},
		{36, 114, 200, 255}, {188, 63, 188, 255}, {17, 168, 205, 255}, {229, 229, 229, 255},
		{102, 102, 102, 255}, {241, 76, 76, 255}, {35, 209, 139, 255}, {245, 245, 67, 255},
		{59, 142, 234, 255}, {214, 112, 214, 255}, {41, 184, 219, 255}, {255, 255, 255, 255},
	}
	switch {
	case n < 16:
		return basic[n]
	case n < 232:
		n -= 16
		steps := []uint8{0, 95, 135, 175, 215, 255}
		return color.RGBA{steps[(n/36)%6], steps[(n/6)%6], steps[n%6], 255}
	default:
		v := uint8(8 + (n-232)*10)
		return color.RGBA{v, v, v, 255}
	}
}

// parseFrame turns an ANSI-styled terminal string into a grid of cells.
func parseFrame(s string) [][]cell {
	var grid [][]cell
	var row []cell
	fg, bg := defaultFG, defaultBG
	newline := func() {
		grid = append(grid, row)
		row = nil
	}
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' {
			newline()
			i += size
			continue
		}
		if r == 0x1b && i+1 < len(s) && s[i+1] == '[' { // CSI
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				fg, bg = applySGR(s[i+2:j], fg, bg)
			}
			i = j + 1
			continue
		}
		row = append(row, cell{ch: substitute(r), fg: fg, bg: bg})
		i += size
	}
	if row != nil {
		newline()
	}
	return grid
}

func applySGR(params string, fg, bg color.RGBA) (color.RGBA, color.RGBA) {
	if params == "" {
		return defaultFG, defaultBG
	}
	parts := strings.Split(params, ";")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		nums = append(nums, n)
	}
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		switch {
		case n == 0:
			fg, bg = defaultFG, defaultBG
		case n == 7: // reverse
			fg, bg = bg, fg
		case n == 39:
			fg = defaultFG
		case n == 49:
			bg = defaultBG
		case n >= 30 && n <= 37:
			fg = xterm256(n - 30)
		case n >= 90 && n <= 97:
			fg = xterm256(n - 90 + 8)
		case n >= 40 && n <= 47:
			bg = xterm256(n - 40)
		case n >= 100 && n <= 107:
			bg = xterm256(n - 100 + 8)
		case n == 38 || n == 48:
			c := fg
			if i+2 < len(nums) && nums[i+1] == 5 {
				c = xterm256(nums[i+2])
				i += 2
			} else if i+4 < len(nums) && nums[i+1] == 2 {
				c = color.RGBA{uint8(nums[i+2]), uint8(nums[i+3]), uint8(nums[i+4]), 255}
				i += 4
			}
			if n == 38 {
				fg = c
			} else {
				bg = c
			}
		}
	}
	return fg, bg
}

// renderFrame rasterizes a grid to an RGBA image of fixed cols x rows.
func renderFrame(grid [][]cell, cols, rows int) *image.RGBA {
	w := cols*cellW + 2*padding
	h := rows*cellH + 2*padding
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, defaultBG)
	for ry := 0; ry < rows && ry < len(grid); ry++ {
		line := grid[ry]
		for cx := 0; cx < cols && cx < len(line); cx++ {
			drawCell(img, cx, ry, line[cx])
		}
	}
	return img
}

func fill(img *image.RGBA, c color.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
}

func drawCell(img *image.RGBA, cx, ry int, c cell) {
	x0 := padding + cx*cellW
	y0 := padding + ry*cellH
	if c.bg != defaultBG {
		for y := 0; y < cellH; y++ {
			for x := 0; x < cellW; x++ {
				img.SetRGBA(x0+x, y0+y, c.bg)
			}
		}
	}
	if c.ch < 0x20 || c.ch > 0x7e {
		return
	}
	glyph := font8x8[c.ch]
	for gy := 0; gy < 8; gy++ {
		bits := glyph[gy]
		for gx := 0; gx < 8; gx++ {
			if bits&(1<<gx) == 0 {
				continue
			}
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					img.SetRGBA(x0+gx*scale+sx, y0+gy*scale+sy, c.fg)
				}
			}
		}
	}
}
