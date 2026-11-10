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

// Axis is one dimension of the flavour fingerprint. Score is already scaled to
// 0..100 and is used directly as a fraction of the radar radius.
type Axis struct {
	Label string
	Score float64
}

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

	// Year scopes a recap card to one calendar year. It is ignored by the
	// other kinds, which describe the whole history.
	Year int
	// UniqueDrinks, Badges, TopVenue, and TopStyle feed the recap body line.
	UniqueDrinks int
	Badges       int
	TopVenue     string
	TopStyle     string
	// Axes feeds the flavour fingerprint radar. Fewer than three axes render
	// as a bar list instead.
	Axes []Axis

	Kind        string
	GeneratedAt time.Time
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

	kind := normalizeKind(data.Kind)

	// Warm fizz rings on the right, kept behind all copy and inside the panel.
	// The fingerprint replaces them with its own rings, so they are skipped
	// there rather than fighting the chart.
	if kind != "fingerprint" {
		fillCircle(img, 980, 285, 190, accentWash)
		strokeCircle(img, 980, 285, 132, 3, accentRing)
		fillCircle(img, 1124, 104, 6, accentRing)
	}

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

	eyebrow := "SODA PASSPORT"
	switch kind {
	case "streak":
		eyebrow = "SODA STREAK"
	case "recap":
		eyebrow = "YEAR IN SODA"
	case "fingerprint":
		eyebrow = "FLAVOUR FINGERPRINT"
	}
	drawTrackedRight(img, eyebrowFace, eyebrow, cardWidth-padX, padTop+20, 3, accentColor)

	// Identity sits above the stat shelf. Resolved once so every kind falls
	// back to the handle and then to a neutral name.
	name := strings.TrimSpace(data.DisplayName)
	if name == "" {
		name = strings.TrimSpace(data.Handle)
	}
	if name == "" {
		name = "Sodapop member"
	}

	switch kind {
	case "fingerprint":
		nameHeadline := mustFace(fnt, 62)
		drawText(img, nameHeadline, fitText(nameHeadline, name, 700), padX, 244, inkColor)
		if handle := strings.TrimSpace(data.Handle); handle != "" {
			drawText(img, handleFace, fitText(handleFace, "@"+handle, 700), padX+2, 282, mutedColor)
		}
		drawRadar(img, fnt, data.Axes)
	case "recap":
		recapFace := mustFace(fnt, 132)
		drawText(img, recapFace, formatYear(recapYear(data)), padX, 316, inkColor)
		// The summary is optional detail: drop trailing segments rather than
		// ellipsizing mid-word when the year has a lot to say.
		parts := recapSummaryParts(data)
		for len(parts) > 0 && measure(labelFace, strings.Join(parts, " · ")) > cardWidth-2*padX {
			parts = parts[:len(parts)-1]
		}
		if len(parts) > 0 {
			drawText(img, labelFace, strings.Join(parts, " · "), padX+4, 360, mutedColor)
		}
		drawIdentity(img, nameFace, handleFace, data, name, padX)
		drawShelf(img, statFace, statLabelFace, data, kind, padX)
	default:
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
		drawIdentity(img, nameFace, handleFace, data, name, padX)
		drawShelf(img, statFace, statLabelFace, data, kind, padX)
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

// normalizeKind maps the stored kind onto one of the four renderers. Unknown
// kinds keep the old, safe passport rendering.
func normalizeKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "streak":
		return "streak"
	case "recap":
		return "recap"
	case "fingerprint":
		return "fingerprint"
	default:
		return "passport"
	}
}

// drawIdentity writes the member name and handle above the stat shelf.
func drawIdentity(dst *image.RGBA, nameFace, handleFace font.Face, data Data, name string, padX int) {
	const maxNameWidth = 700
	drawText(dst, nameFace, fitText(nameFace, name, maxNameWidth), padX, 428, inkColor)
	if handle := strings.TrimSpace(data.Handle); handle != "" {
		drawText(dst, handleFace, fitText(handleFace, "@"+handle, maxNameWidth), padX+2, 462, mutedColor)
	}
}

// drawShelf draws the row of small totals. A recap keeps up to five cells so
// the best streak fits; the other kinds stay at four.
func drawShelf(dst *image.RGBA, statFace, statLabelFace font.Face, data Data, kind string, padX int) {
	cells := statCells(data, kind)
	if len(cells) == 0 {
		return
	}
	fillRect(dst, padX, 488, cardWidth-padX, 490, softRule)
	gap := 28
	cellWidth := (cardWidth - 2*padX - gap*(len(cells)-1)) / len(cells)
	if cellWidth > 260 {
		cellWidth = 260
	}
	for i, cell := range cells {
		x := padX + i*(cellWidth+gap)
		drawText(dst, statFace, formatNumber(cell.Value), x, 530, inkColor)
		drawTracked(dst, statLabelFace, strings.ToUpper(cell.Label), x, 554, 2, mutedColor)
	}
}

// statCells reorders the summary for the kind. Zero values are omitted so a new
// account does not show a wall of dashes, except on a recap with nothing in it:
// that still renders its year and explicit zeros.
func statCells(data Data, kind string) []statCell {
	var cells []statCell
	limit := 4
	switch kind {
	case "streak":
		cells = []statCell{
			{"Check-ins", data.Checkins},
			{"Styles", data.Styles},
			{"Venue kinds", data.VenueKinds},
			{"Countries", data.Countries},
			{"Longest streak", data.LongestStreak},
		}
	case "recap":
		limit = 5
		cells = []statCell{
			{"Drinks", data.UniqueDrinks},
			{"Check-ins", data.Checkins},
			{"Styles", data.Styles},
			{"Badges", data.Badges},
			{"Best streak", data.LongestStreak},
		}
	default:
		cells = []statCell{
			{"Styles", data.Styles},
			{"Venue kinds", data.VenueKinds},
			{"Countries", data.Countries},
			{"Longest streak", data.LongestStreak},
		}
	}
	out := make([]statCell, 0, limit)
	for _, cell := range cells {
		if cell.Value <= 0 {
			continue
		}
		out = append(out, cell)
		if len(out) == limit {
			break
		}
	}
	if len(out) == 0 && kind == "recap" {
		out = []statCell{
			{"Drinks", 0},
			{"Check-ins", 0},
		}
	}
	return out
}

// recapYear returns the calendar year a recap card describes, falling back to
// the generation date for a caller that forgot to set it.
func recapYear(data Data) int {
	if data.Year > 0 {
		return data.Year
	}
	generated := data.GeneratedAt
	if generated.IsZero() {
		generated = time.Now().UTC()
	}
	return generated.Year()
}

// recapSummaryParts builds "N drinks · N styles · N badges" defensively: zero
// parts are dropped and a top style or venue is appended only when known.
func recapSummaryParts(data Data) []string {
	parts := make([]string, 0, 4)
	if n := data.UniqueDrinks; n > 0 {
		parts = append(parts, pluralCount(n, "drink"))
	}
	if n := data.Styles; n > 0 {
		parts = append(parts, pluralCount(n, "style"))
	}
	if n := data.Badges; n > 0 {
		parts = append(parts, pluralCount(n, "badge"))
	}
	switch {
	case data.TopStyle != "" && data.TopVenue != "":
		parts = append(parts, "favourite "+data.TopStyle+" at "+data.TopVenue)
	case data.TopStyle != "":
		parts = append(parts, "favourite "+data.TopStyle)
	case data.TopVenue != "":
		parts = append(parts, "most visits at "+data.TopVenue)
	}
	return parts
}

// recapSummary joins the parts for callers that want the whole line.
func recapSummary(data Data) string {
	return strings.Join(recapSummaryParts(data), " · ")
}

func pluralCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return formatNumber(n) + " " + noun + "s"
}

// formatYear prints a calendar year without the thousands separator
// formatNumber adds for counts.
func formatYear(year int) string {
	return fmt.Sprintf("%d", year)
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
