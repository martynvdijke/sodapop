package sharecard

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strings"

	"golang.org/x/image/font/sfnt"
)

// The radar sits in the right half of the panel so the score list has room on
// the left. Both stay inside the 1200x630 card and its footer rule at y=566.
const (
	radarCenterX  = 880.0
	radarCenterY  = 410.0
	radarRadius   = 108.0
	radarLabelGap = 22.0
	radarMaxAxes  = 5
)

// radarFill is the 55% wash inside the polygon. The outline and the vertex
// dots stay fully opaque on top of it.
var radarFill = color.RGBA{R: 0xF0, G: 0x8C, B: 0x3E, A: 0x8C}

// radarPoint is a float coordinate in card pixels.
type radarPoint struct {
	X float64
	Y float64
}

// drawRadar renders the flavour fingerprint. Three or more axes get a polygon
// on concentric rings; one or two degrade to a horizontal bar list in the same
// region so a sparse profile still reads as a card rather than a broken chart.
func drawRadar(dst *image.RGBA, fnt *sfnt.Font, axes []Axis) {
	if len(axes) > radarMaxAxes {
		axes = axes[:radarMaxAxes]
	}
	switch {
	case len(axes) == 0:
		noteFace := mustFace(fnt, 26)
		drawText(dst, noteFace, "Not enough check-ins yet for a fingerprint.", 84, 400, mutedColor)
		return
	case len(axes) < 3:
		drawAxisBars(dst, fnt, axes)
		return
	}

	n := len(axes)
	cx, cy, r := radarCenterX, radarCenterY, radarRadius

	// Reference rings and spokes, faint enough to sit behind the shape.
	for _, frac := range []float64{0.25, 0.5, 0.75, 1} {
		ring := make([]radarPoint, n)
		for i := range axes {
			a := radarAngle(i, n)
			ring[i] = radarPoint{X: cx + math.Cos(a)*r*frac, Y: cy + math.Sin(a)*r*frac}
		}
		strokePolygon(dst, ring, 1, softRule)
	}
	for i := range axes {
		a := radarAngle(i, n)
		drawLine(dst,
			int(math.Round(cx)), int(math.Round(cy)),
			int(math.Round(cx+math.Cos(a)*r)), int(math.Round(cy+math.Sin(a)*r)),
			1, softRule)
	}

	shape := make([]radarPoint, n)
	for i, axis := range axes {
		a := radarAngle(i, n)
		rr := r * clampScore(axis.Score) / 100
		shape[i] = radarPoint{X: cx + math.Cos(a)*rr, Y: cy + math.Sin(a)*rr}
	}
	fillPolygon(dst, shape, radarFill)
	strokePolygon(dst, shape, 2, accentColor)
	for _, p := range shape {
		fillCircle(dst, int(math.Round(p.X)), int(math.Round(p.Y)), 4, accentColor)
	}

	axisFace := mustFace(fnt, 20)
	scoreFace := mustFace(fnt, 22)
	for i, axis := range axes {
		label := axisLabel(axis)

		a := radarAngle(i, n)
		px := cx + math.Cos(a)*(r+radarLabelGap)
		py := cy + math.Sin(a)*(r+radarLabelGap)
		trimmed := fitText(axisFace, label, 150)
		drawText(dst, axisFace, trimmed, int(math.Round(px))-measure(axisFace, trimmed)/2, int(math.Round(py))+7, mutedColor)

		// Score list on the left: label, proportional bar, value.
		rowY := 360 + i*44
		drawText(dst, scoreFace, fitText(scoreFace, label, 150), 84, rowY, inkColor)
		drawScoreBar(dst, 300, rowY-9, 200, 8, axis.Score)
		drawTrackedRight(dst, scoreFace, formatNumber(int(math.Round(clampScore(axis.Score)))), 560, rowY, 0, mutedColor)
	}
}

// drawAxisBars is the degraded layout for one or two axes: no polygon can be
// drawn, so the same region becomes a set of readable bars.
func drawAxisBars(dst *image.RGBA, fnt *sfnt.Font, axes []Axis) {
	labelFace := mustFace(fnt, 32)
	scoreFace := mustFace(fnt, 32)
	const step = 62
	top := 400 - (len(axes)-1)*step/2
	for i, axis := range axes {
		y := top + i*step
		drawText(dst, labelFace, fitText(labelFace, axisLabel(axis), 260), 84, y, inkColor)
		drawScoreBar(dst, 380, y-14, 560, 16, axis.Score)
		drawTrackedRight(dst, scoreFace, formatNumber(int(math.Round(clampScore(axis.Score)))), 1116, y, 0, mutedColor)
	}
}

// axisLabel keeps an unlabelled axis printable without breaking the layout.
func axisLabel(axis Axis) string {
	label := strings.TrimSpace(axis.Label)
	if label == "" {
		return "Other"
	}
	return label
}

// clampScore keeps a caller-supplied score inside the 0..100 radius fraction
// and folds NaN back to zero.
func clampScore(score float64) float64 {
	if math.IsNaN(score) || score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// radarAngle starts the first axis at twelve o'clock and walks clockwise.
func radarAngle(i, n int) float64 {
	return -math.Pi/2 + 2*math.Pi*float64(i)/float64(n)
}

func drawScoreBar(dst *image.RGBA, x, y, width, height int, score float64) {
	fillRect(dst, x, y, x+width, y+height, softRule)
	filled := int(math.Round(float64(width) * clampScore(score) / 100))
	if filled > 0 {
		fillRect(dst, x, y, x+filled, y+height, accentColor)
	}
}

// fillPolygon fills a closed polygon with a scanline pass, blending each pixel
// so the wash keeps the panel visible underneath.
func fillPolygon(dst *image.RGBA, pts []radarPoint, c color.RGBA) {
	if len(pts) < 3 {
		return
	}
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	y0 := int(math.Floor(minY))
	y1 := int(math.Ceil(maxY))
	crossings := make([]float64, 0, len(pts))
	for y := y0; y <= y1; y++ {
		yc := float64(y) + 0.5
		crossings = crossings[:0]
		for i := range pts {
			a := pts[i]
			b := pts[(i+1)%len(pts)]
			if (a.Y <= yc && b.Y > yc) || (b.Y <= yc && a.Y > yc) {
				t := (yc - a.Y) / (b.Y - a.Y)
				crossings = append(crossings, a.X+t*(b.X-a.X))
			}
		}
		if len(crossings) < 2 {
			continue
		}
		sort.Float64s(crossings)
		for i := 0; i+1 < len(crossings); i += 2 {
			xs := int(math.Round(crossings[i]))
			xe := int(math.Round(crossings[i+1]))
			for x := xs; x < xe; x++ {
				if image.Pt(x, y).In(dst.Bounds()) {
					blendPixel(dst, x, y, c)
				}
			}
		}
	}
}

func strokePolygon(dst *image.RGBA, pts []radarPoint, thickness int, c color.RGBA) {
	if len(pts) < 2 {
		return
	}
	for i := range pts {
		a := pts[i]
		b := pts[(i+1)%len(pts)]
		drawLine(dst,
			int(math.Round(a.X)), int(math.Round(a.Y)),
			int(math.Round(b.X)), int(math.Round(b.Y)),
			thickness, c)
	}
}

// drawLine is a Bresenham line with integer thickness, which is enough for a
// chart this small and avoids pulling in a rasterizer dependency.
func drawLine(dst *image.RGBA, x0, y0, x1, y1, thickness int, c color.RGBA) {
	if thickness < 1 {
		thickness = 1
	}
	offset := thickness / 2
	for dx := -offset; dx <= offset; dx++ {
		for dy := -offset; dy <= offset; dy++ {
			bresenham(dst, x0+dx, y0+dy, x1+dx, y1+dy, c)
		}
	}
}

func bresenham(dst *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := absInt(x1 - x0)
	dy := -absInt(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(dst.Bounds()) {
			blendPixel(dst, x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
