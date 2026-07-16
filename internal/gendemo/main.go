// Command gendemo stitches the terminal screenshots in docs/screenshots into
// the animated GIF shown at the top of the README. The shots are real
// captures of the real binary driving real disks on the test VM, numbered in
// flow order, so the demo shows what the tool actually does rather than a
// reconstruction of it.
//
// Usage: go run ./internal/gendemo [out.gif] [screenshot-dir]
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Hold each frame long enough to read it, in centiseconds, keyed by
// screenshot number; busier screens get longer. Unlisted frames use
// defaultHold, so adding a shot needs no edit here.
var holds = map[int]int{
	2:  300, // every blocker on the system disk at once
	6:  240, // the first selectable device
	7:  320, // the filesystem descriptions
	8:  260, // the options form and its help pane
	12: 300, // real mkfs output
	13: 240, // back at the list, now ext4
	14: 360, // device information, and the loop's pause before it restarts
}

const defaultHold = 180

// A terminal renders a fixed handful of colors; the rest of what a JPEG
// carries is compression noise. Keeping the palette small forces those back
// onto the real colors and costs the encoder fewer bits per pixel.
const maxColors = 32

func main() {
	out := "docs/demo.gif"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	src := "docs/screenshots"
	if len(os.Args) > 2 {
		src = os.Args[2]
	}

	paths, err := shotPaths(src)
	if err != nil {
		fatal(err)
	}
	if len(paths) == 0 {
		fatal(fmt.Errorf("no numbered .jpg screenshots in %s", src))
	}

	imgs := make([]image.Image, len(paths))
	delays := make([]int, len(paths))
	for i, p := range paths {
		if imgs[i], err = decode(p); err != nil {
			fatal(fmt.Errorf("%s: %w", p, err))
		}
		n, _ := shotNumber(p)
		if d, ok := holds[n]; ok {
			delays[i] = d
		} else {
			delays[i] = defaultHold
		}
	}

	// GIF requires one size; the shots should already agree, but pad rather
	// than crop if a later capture is off by a few pixels.
	size := image.Rectangle{}
	for _, im := range imgs {
		size = size.Union(im.Bounds().Sub(im.Bounds().Min))
	}

	pal := buildPalette(imgs)
	frames := make([]*image.Paletted, len(imgs))
	for i, im := range imgs {
		frames[i] = toPaletted(im, size, pal)
	}
	frames = diff(frames)

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	disposals := make([]byte, len(frames))
	for i := range disposals {
		disposals[i] = gif.DisposalNone // each frame paints over the last
	}
	g := &gif.GIF{
		Image:     frames,
		Delay:     delays,
		Disposal:  disposals,
		LoopCount: 0,
		Config:    image.Config{ColorModel: pal, Width: size.Dx(), Height: size.Dy()},
	}
	if err := gif.EncodeAll(f, g); err != nil {
		fatal(err)
	}
	fi, _ := f.Stat()
	fmt.Printf("%s: %d frames, %dx%d, %d colors, %.0f KiB\n",
		out, len(frames), size.Dx(), size.Dy(), len(pal), float64(fi.Size())/1024)
}

// shotPaths returns the numbered screenshots in numeric order — 10.jpg sorts
// after 9.jpg, which a lexical sort gets wrong.
func shotPaths(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		if _, ok := shotNumber(m); ok {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := shotNumber(out[i])
		b, _ := shotNumber(out[j])
		return a < b
	})
	return out, nil
}

func shotNumber(path string) (int, bool) {
	base := filepath.Base(path)
	n, err := strconv.Atoi(strings.TrimSuffix(base, filepath.Ext(base)))
	return n, err == nil
}

func decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jpeg.Decode(f)
}

// JPEG smears every flat terminal color across a cloud of near-identical
// neighbours. Bucketing collapses that cloud back to one entry so the palette
// spends its slots on real colors instead of compression noise — and mapping
// through it denoises the frame as a side effect, which is what lets diff()
// see two captures of an unchanged region as unchanged.
const bucket = 16

func quantize(c color.RGBA) color.RGBA {
	q := func(v uint8) uint8 {
		n := (int(v) + bucket/2) / bucket * bucket
		if n > 255 {
			n = 255
		}
		return uint8(n)
	}
	return color.RGBA{q(c.R), q(c.G), q(c.B), 255}
}

func rgba(c color.Color) color.RGBA {
	r, g, b, _ := c.RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
}

// buildPalette takes the most common bucketed colors across every frame. A
// terminal screenshot only holds a handful of real colors, so the common ones
// are exactly those and the long tail is noise.
func buildPalette(imgs []image.Image) color.Palette {
	counts := map[color.RGBA]int{}
	for _, im := range imgs {
		b := im.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				counts[quantize(rgba(im.At(x, y)))]++
			}
		}
	}
	type entry struct {
		c color.RGBA
		n int
	}
	entries := make([]entry, 0, len(counts))
	for c, n := range counts {
		entries = append(entries, entry{c, n})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].n != entries[j].n {
			return entries[i].n > entries[j].n
		}
		// Ties must break deterministically or the GIF churns between runs.
		a, b := entries[i].c, entries[j].c
		return uint32(a.R)<<16|uint32(a.G)<<8|uint32(a.B) < uint32(b.R)<<16|uint32(b.G)<<8|uint32(b.B)
	})
	// The last slot is reserved for transparency, which diff() paints
	// unchanged pixels with.
	if len(entries) > maxColors-1 {
		entries = entries[:maxColors-1]
	}
	pal := make(color.Palette, len(entries)+1)
	for i, e := range entries {
		pal[i] = e.c
	}
	pal[len(entries)] = color.RGBA{}
	return pal
}

func toPaletted(im image.Image, size image.Rectangle, pal color.Palette) *image.Paletted {
	// Pad onto black so a short frame does not inherit the previous one.
	canvas := image.NewRGBA(size)
	draw.Draw(canvas, size, image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.Draw(canvas, im.Bounds().Sub(im.Bounds().Min), im, im.Bounds().Min, draw.Src)

	// Nearest-match is O(len(pal)) per pixel; cache on the bucketed color, of
	// which there are few, so the search runs once per distinct color. Search
	// only the opaque entries — the last slot is diff()'s transparent one and
	// is never a legitimate match for a visible pixel.
	opaque := pal[:len(pal)-1]
	idx := map[color.RGBA]uint8{}
	out := image.NewPaletted(size, pal)
	for y := size.Min.Y; y < size.Max.Y; y++ {
		for x := size.Min.X; x < size.Max.X; x++ {
			q := quantize(rgba(canvas.At(x, y)))
			i, ok := idx[q]
			if !ok {
				i = uint8(opaque.Index(q))
				idx[q] = i
			}
			out.SetColorIndex(x, y, i)
		}
	}
	return out
}

// diff rewrites every frame after the first to carry only what changed:
// pixels equal to the previous frame become transparent, and the frame is
// cropped to the region that moved. Consecutive screens differ by a
// highlighted row and a line of footer text, so most of each frame becomes a
// run of one index — which is what LZW is good at.
func diff(frames []*image.Paletted) []*image.Paletted {
	if len(frames) < 2 {
		return frames
	}
	clear := uint8(len(frames[0].Palette) - 1) // the transparent slot
	out := []*image.Paletted{frames[0]}
	for i := 1; i < len(frames); i++ {
		prev, cur := frames[i-1], frames[i]
		box := image.Rectangle{Min: cur.Bounds().Max, Max: cur.Bounds().Min}
		for y := cur.Bounds().Min.Y; y < cur.Bounds().Max.Y; y++ {
			for x := cur.Bounds().Min.X; x < cur.Bounds().Max.X; x++ {
				if cur.ColorIndexAt(x, y) != prev.ColorIndexAt(x, y) {
					box = box.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if box.Empty() { // identical frame: keep a 1x1 stub so the delay holds
			box = image.Rect(0, 0, 1, 1)
		}
		f := image.NewPaletted(box, cur.Palette)
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				if c := cur.ColorIndexAt(x, y); c != prev.ColorIndexAt(x, y) {
					f.SetColorIndex(x, y, c)
				} else {
					f.SetColorIndex(x, y, clear)
				}
			}
		}
		out = append(out, f)
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gendemo:", err)
	os.Exit(1)
}
