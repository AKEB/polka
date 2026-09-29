// Package covergen draws placeholder book covers when a file has no art.
package covergen

import (
	"bytes"
	"embed"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed LiberationSans-Regular.ttf LiberationSans-Bold.ttf
var fontFS embed.FS

const (
	coverW = 400
	coverH = 600
)

var (
	fontsOnce sync.Once
	faceReg   font.Face
	faceBold  font.Face
	fontErr   error
)

func loadFaces() {
	fontsOnce.Do(func() {
		reg, err := fontFS.ReadFile("LiberationSans-Regular.ttf")
		if err != nil {
			fontErr = err
			return
		}
		bold, err := fontFS.ReadFile("LiberationSans-Bold.ttf")
		if err != nil {
			fontErr = err
			return
		}
		rt, err := opentype.Parse(reg)
		if err != nil {
			fontErr = err
			return
		}
		bt, err := opentype.Parse(bold)
		if err != nil {
			fontErr = err
			return
		}
		faceReg, err = opentype.NewFace(rt, &opentype.FaceOptions{Size: 22, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			fontErr = err
			return
		}
		faceBold, err = opentype.NewFace(bt, &opentype.FaceOptions{Size: 28, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			fontErr = err
			return
		}
	})
}

// PNG returns a 400×600 PNG with the title and authors on a deterministic
// palette derived from the text (so the same book always gets the same cover).
func PNG(title, authors string) ([]byte, error) {
	loadFaces()
	if fontErr != nil {
		return nil, fontErr
	}
	title = strings.TrimSpace(title)
	authors = strings.TrimSpace(authors)
	if title == "" {
		title = "—"
	}

	bg, ink, accent := palette(title + "\n" + authors)
	img := image.NewRGBA(image.Rect(0, 0, coverW, coverH))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	// Soft vertical vignette toward the edges.
	for y := 0; y < coverH; y++ {
		for x := 0; x < 18; x++ {
			a := uint8(30 * (18 - x) / 18)
			shade(img, x, y, a)
			shade(img, coverW-1-x, y, a)
		}
	}

	// Accent band under the top margin.
	for y := 48; y < 54; y++ {
		for x := 36; x < coverW-36; x++ {
			img.Set(x, y, accent)
		}
	}

	const padX = 36
	maxWidth := coverW - 2*padX

	titleLines := wrap(faceBold, title, maxWidth)
	authorLines := wrap(faceReg, authors, maxWidth)

	titleH := len(titleLines) * faceBold.Metrics().Height.Ceil()
	authorH := 0
	if authors != "" {
		authorH = 24 + len(authorLines)*faceReg.Metrics().Height.Ceil()
	}
	blockH := titleH + authorH
	y := (coverH - blockH) / 2
	if y < 80 {
		y = 80
	}

	y = drawLines(img, faceBold, titleLines, padX, maxWidth, y, ink)
	if authors != "" {
		y += 20
		for x := coverW/2 - 24; x < coverW/2+24; x++ {
			img.Set(x, y, accent)
		}
		y += 18
		drawLines(img, faceReg, authorLines, padX, maxWidth, y, ink)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func shade(img *image.RGBA, x, y int, a uint8) {
	c := img.RGBAAt(x, y)
	img.SetRGBA(x, y, color.RGBA{
		R: darken(c.R, a), G: darken(c.G, a), B: darken(c.B, a), A: 255,
	})
}

func darken(v, a uint8) uint8 {
	d := int(v) - int(a)
	if d < 0 {
		return 0
	}
	return uint8(d)
}

func drawLines(img *image.RGBA, face font.Face, lines []string, padX, maxWidth, y int, ink color.Color) int {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(ink), Face: face}
	lineH := face.Metrics().Height.Ceil()
	for _, line := range lines {
		w := font.MeasureString(face, line).Ceil()
		x := padX + (maxWidth-w)/2
		if x < padX {
			x = padX
		}
		d.Dot = fixed.P(x, y+face.Metrics().Ascent.Ceil())
		d.DrawString(line)
		y += lineH
	}
	return y
}

func wrap(face font.Face, text string, maxWidth int) []string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	var lines []string
	var cur string
	for _, w := range words {
		trial := w
		if cur != "" {
			trial = cur + " " + w
		}
		if font.MeasureString(face, trial).Ceil() <= maxWidth {
			cur = trial
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		// A single overlong word: hard-slice by runes.
		if font.MeasureString(face, w).Ceil() > maxWidth {
			for _, chunk := range splitWide(face, w, maxWidth) {
				lines = append(lines, chunk)
			}
			cur = ""
		} else {
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	const maxLines = 8
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		last := lines[maxLines-1]
		if r := []rune(last); len(r) > 1 {
			lines[maxLines-1] = string(r[:len(r)-1]) + "…"
		}
	}
	return lines
}

func splitWide(face font.Face, word string, maxWidth int) []string {
	var out []string
	var cur []rune
	for _, r := range word {
		trial := string(append(cur, r))
		if len(cur) > 0 && font.MeasureString(face, trial).Ceil() > maxWidth {
			out = append(out, string(cur))
			cur = []rune{r}
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// palette picks a muted paper-like background and matching ink/accent.
func palette(seed string) (bg, ink, accent color.RGBA) {
	h := fnv.New32a()
	fmt.Fprint(h, seed)
	n := h.Sum32()

	// Hues spaced around the wheel, kept desaturated and mid-light.
	hues := []struct{ r, g, b uint8 }{
		{214, 196, 176}, // warm sand
		{196, 208, 198}, // sage
		{198, 204, 216}, // slate blue
		{216, 198, 198}, // dusty rose
		{208, 204, 184}, // olive paper
		{188, 200, 208}, // cool grey-blue
		{212, 204, 192}, // parchment
		{200, 192, 208}, // lilac grey
	}
	c := hues[int(n)%len(hues)]
	bg = color.RGBA{R: c.r, G: c.g, B: c.b, A: 255}
	ink = color.RGBA{R: 36, G: 32, B: 28, A: 255}
	accent = color.RGBA{
		R: uint8(40 + n%80),
		G: uint8(50 + (n>>8)%70),
		B: uint8(45 + (n>>16)%80),
		A: 255,
	}
	return bg, ink, accent
}
