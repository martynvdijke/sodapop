// Package sharecard renders the 1200x630 PNG social card for passport and
// streak share links. It uses the Go font family embedded in x/image, so the
// binary needs no font assets on disk.
package sharecard

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	cardWidth  = 1200
	cardHeight = 630
)

// Data is everything the card needs. Empty or zero values are skipped rather
// than rendered as blank cells.
type Data struct {
	Handle        string
	DisplayName   string
	Checkins      int
	Styles        int
	Countries     int
	VenueKinds    int
	CurrentStreak int
	LongestStreak int
	Kind          string
	GeneratedAt   time.Time
}

var (
	bgColor      = color.RGBA{R: 0xF2, G: 0xF4, B: 0xE6, A: 0xFF}
	panelColor   = color.RGBA{R: 0x0C, G: 0x13, B: 0x0E, A: 0xFF}
	inkColor     = color.RGBA{R: 0xF2, G: 0xF4, B: 0xE6, A: 0xFF}
	mutedColor   = color.RGBA{R: 0xA8, G: 0xB5, B: 0xA5, A: 0xFF}
	accentColor  = color.RGBA{R: 0xF0, G: 0x8C, B: 0x3E, A: 0xFF}
	softRule     = color.RGBA{R: 0xF2, G: 0xF4, B: 0xE6, A: 0x2E}
	accentWash   = color.RGBA{R: 0xF0, G: 0x8C, B: 0x3E, A: 0x1E}
	accentRing   = color.RGBA{R: 0xF0, G: 0x8C, B: 0x3E, A: 0x44}
	measureOnce  sync.Once
	parsedFont   *sfnt.Font
	parseFontErr error
)

// Render produces a PNG-encoded card. It never panics on missing data: empty
// names, zero totals, and unknown kinds still render a complete card.
func Render(data Data) ([]byte, error) {
	fnt, err := baseFont()
	if err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, cardWidth, cardHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)
	fillRoundRect(img, 24, 24, cardWidth-24, cardHeight-24, 34, panelColor)

	// Warm fizz rings on the right, kept behind all copy and inside the panel.
	fillCircle(img, 980, 285, 190, accentWash)
	strokeCircle(img, 980, 285, 132, 3, accentRing)
	fillCircle(img, 1124, 104, 6, accentRing)

	const (
		padX      = 84
		padTop    = 78
		statLabel = 20.0
	)

	brandFace := mustFace(fnt, 30)
	eyebrowFace := mustFace(fnt, 20)
	headlineFace := mustFace(fnt, 188)
	labelFace := mustFace(fnt, 34)
	nameFace := mustFace(fnt, 38)
	handleFace := mustFace(fnt, 26)
	statFace := mustFace(fnt, 40)
	statLabelFace := mustFace(fnt, statLabel)
	footerFace := mustFace(fnt, 20)

	// Brand lockup.
	fillCircle(img, padX+10, padTop+10, 10, accentColor)
	drawText(img, brandFace, "Sodapop", padX+34, padTop+22, inkColor)

	kind := strings.ToLower(strings.TrimSpace(data.Kind))
	if kind != "streak" {
		kind = "passport"
	}
	eyebrow := "SODA PASSPORT"
	if kind == "streak" {
		eyebrow = "SODA STREAK"
	}
	drawTrackedRight(img, eyebrowFace, eyebrow, cardWidth-padX, padTop+20, 3, accentColor)

	headline := data.Checkins
	headlineLabel := "check-ins logged"
	if kind == "streak" {
		headline = data.CurrentStreak
		headlineLabel = "days in a row"
		if headline == 1 {
			headlineLabel = "day in a row"
		}
	} else if headline == 1 {
		headlineLabel = "check-in logged"
	}
	drawText(img, headlineFace, formatNumber(headline), padX, 322, inkColor)
	drawText(img, labelFace, headlineLabel, padX+4, 372, mutedColor)

	// Identity sits above the stat shelf.
	name := strings.TrimSpace(data.DisplayName)
	if name == "" {
		name = strings.TrimSpace(data.Handle)
	}
	if name == "" {
		name = "Sodapop member"
	}
	maxNameWidth := 700
	drawText(img, nameFace, fitText(nameFace, name, maxNameWidth), padX, 428, inkColor)
	if handle := strings.TrimSpace(data.Handle); handle != "" {
		drawText(img, handleFace, fitText(handleFace, "@"+handle, maxNameWidth), padX+2, 462, mutedColor)
	}

	cells := statCells(data, kind)
	if len(cells) > 0 {
		fillRect(img, padX, 488, cardWidth-padX, 490, softRule)
		gap := 28
		cellWidth := (cardWidth - 2*padX - gap*(len(cells)-1)) / len(cells)
		if cellWidth > 260 {
			cellWidth = 260
		}
		for i, cell := range cells {
			x := padX + i*(cellWidth+gap)
			drawText(img, statFace, formatNumber(cell.Value), x, 530, inkColor)
			drawTracked(img, statLabelFace, strings.ToUpper(cell.Label), x, 554, 2, mutedColor)
		}
	}

	fillRect(img, padX, 566, cardWidth-padX, 568, softRule)
	generated := data.GeneratedAt
	if generated.IsZero() {
		generated = time.Now().UTC()
	}
	drawText(img, footerFace, "sodapop", padX, 590, mutedColor)
	drawTrackedRight(img, footerFace, "GENERATED "+strings.ToUpper(generated.UTC().Format("2 Jan 2006")), cardWidth-padX, 590, 2, mutedColor)

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode share card: %w", err)
	}
	return buf.Bytes(), nil
}

type statCell struct {
	Label string
	Value int
}

// statCells reorders the summary for the kind. Zero values are omitted so a new
// account does not show a wall of dashes.
func statCells(data Data, kind string) []statCell {
	var cells []statCell
	if kind == "streak" {
		cells = []statCell{
			{"Check-ins", data.Checkins},
			{"Styles", data.Styles},
			{"Venue kinds", data.VenueKinds},
			{"Countries", data.Countries},
			{"Longest streak", data.LongestStreak},
		}
	} else {
		cells = []statCell{
			{"Styles", data.Styles},
			{"Venue kinds", data.VenueKinds},
			{"Countries", data.Countries},
			{"Longest streak", data.LongestStreak},
		}
	}
	out := make([]statCell, 0, 4)
	for _, cell := range cells {
		if cell.Value <= 0 {
			continue
		}
		out = append(out, cell)
		if len(out) == 4 {
			break
		}
	}
	return out
}

func baseFont() (*sfnt.Font, error) {
	measureOnce.Do(func() {
		parsedFont, parseFontErr = opentype.Parse(goregular.TTF)
	})
	if parseFontErr != nil {
		return nil, fmt.Errorf("parse embedded font: %w", parseFontErr)
	}
	return parsedFont, nil
}

func mustFace(fnt *sfnt.Font, size float64) font.Face {
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		// The embedded font is known-good; fall back to a nil face only to keep
		// Render's signature honest. The caller checks the font above.
		return nil
	}
	return face
}

func drawText(dst *image.RGBA, face font.Face, s string, x, y int, c color.Color) {
	if face == nil || s == "" {
		return
	}
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// drawTracked draws text with extra letter spacing, used for small uppercase
// labels that need a bit of air.
func drawTracked(dst *image.RGBA, face font.Face, s string, x, y, tracking int, c color.Color) {
	if face == nil {
		return
	}
	cursor := x
	for _, r := range s {
		part := string(r)
		drawText(dst, face, part, cursor, y, c)
		cursor += measure(face, part) + tracking
	}
}

func drawTrackedRight(dst *image.RGBA, face font.Face, s string, right, y, tracking int, c color.Color) {
	if face == nil {
		return
	}
	width := 0
	for _, r := range s {
		width += measure(face, string(r)) + tracking
	}
	if width > 0 {
		width -= tracking
	}
	drawTracked(dst, face, s, right-width, y, tracking, c)
}

func measure(face font.Face, s string) int {
	if face == nil {
		return 0
	}
	return font.MeasureString(face, s).Round()
}

// fitText trims a label to maxWidth pixels and appends an ellipsis.
func fitText(face font.Face, s string, maxWidth int) string {
	if face == nil || measure(face, s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := strings.TrimSpace(string(runes)) + "…"
		if measure(face, candidate) <= maxWidth {
			return candidate
		}
	}
	return "…"
}

func formatNumber(n int) string {
	if n <= 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func fillRect(dst *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	rect := image.Rect(x0, y0, x1, y1).Intersect(dst.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			blendPixel(dst, x, y, c)
		}
	}
}

func fillRoundRect(dst *image.RGBA, x0, y0, x1, y1, radius int, c color.RGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	if radius < 0 {
		radius = 0
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if insideRounded(x, y, x0, y0, x1, y1, radius) {
				dst.SetRGBA(x, y, c)
			}
		}
	}
}

func insideRounded(x, y, x0, y0, x1, y1, radius int) bool {
	if x < x0 || x >= x1 || y < y0 || y >= y1 {
		return false
	}
	if radius <= 0 {
		return true
	}
	cx, cy := x, y
	switch {
	case x < x0+radius:
		cx = x0 + radius
	case x >= x1-radius:
		cx = x1 - radius - 1
	default:
		return true
	}
	switch {
	case y < y0+radius:
		cy = y0 + radius
	case y >= y1-radius:
		cy = y1 - radius - 1
	default:
		return true
	}
	dx, dy := float64(x-cx), float64(y-cy)
	return dx*dx+dy*dy <= float64(radius*radius)
}

func fillCircle(dst *image.RGBA, cx, cy, radius int, c color.RGBA) {
	if radius <= 0 {
		return
	}
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if !image.Pt(x, y).In(dst.Bounds()) {
				continue
			}
			dx, dy := float64(x-cx), float64(y-cy)
			if dx*dx+dy*dy <= float64(radius*radius) {
				blendPixel(dst, x, y, c)
			}
		}
	}
}

func strokeCircle(dst *image.RGBA, cx, cy, radius, thickness int, c color.RGBA) {
	if radius <= 0 || thickness <= 0 {
		return
	}
	inner := radius - thickness
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if !image.Pt(x, y).In(dst.Bounds()) {
				continue
			}
			dx, dy := float64(x-cx), float64(y-cy)
			d2 := dx*dx + dy*dy
			if d2 <= float64(radius*radius) && d2 >= float64(inner*inner) {
				blendPixel(dst, x, y, c)
			}
		}
	}
}

func blendPixel(dst *image.RGBA, x, y int, c color.RGBA) {
	if c.A == 0 {
		return
	}
	if c.A == 0xFF {
		dst.SetRGBA(x, y, c)
		return
	}
	a := float64(c.A) / 255
	old := dst.RGBAAt(x, y)
	mix := func(src, dst uint8) uint8 {
		return uint8(math.Round(float64(src)*a + float64(dst)*(1-a)))
	}
	dst.SetRGBA(x, y, color.RGBA{
		R: mix(c.R, old.R),
		G: mix(c.G, old.G),
		B: mix(c.B, old.B),
		A: old.A,
	})
}
