package web

import "fmt"

// millilitresPerFluidOunce is the US customary fluid ounce. Sodapop stores
// every volume in millilitres and never rewrites it when the display units
// change.
const millilitresPerFluidOunce = 29.5735

// FormatVolume renders a stored millilitre value in the viewer's units.
func FormatVolume(ml int, units string) string {
	if units == "imperial" {
		return fmt.Sprintf("%.1f fl oz", float64(ml)/millilitresPerFluidOunce)
	}
	return fmt.Sprintf("%d ml", ml)
}

// FormatPer100 renders a value expressed per 100 ml in the viewer's units.
// Imperial output is per fluid ounce, scaled from the stored per-100 ml value.
// The value is accepted as any numeric type: Ent stores caffeine as an int and
// sugar as a float, and text/template will not coerce a typed field to a
// different function parameter type.
func FormatPer100(v any, units string) string {
	return formatPer100Label(numericValue(v), "", units)
}

func numericValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	default:
		return 0
	}
}

// templateVolume and templatePer100 are the metric defaults registered on the
// shared templates. Renderer.Render rebinds them per request when the viewer
// prefers imperial units.
func templateVolume(ml int) string { return FormatVolume(ml, "metric") }

func templatePer100(v any, label string) string {
	return formatPer100Label(numericValue(v), label, "metric")
}

func formatPer100Label(v float64, label, units string) string {
	if units == "imperial" {
		if label == "" {
			return fmt.Sprintf("%.1f / fl oz", v*millilitresPerFluidOunce/100)
		}
		return fmt.Sprintf("%.1f %s / fl oz", v*millilitresPerFluidOunce/100, label)
	}
	if label == "" {
		return fmt.Sprintf("%.1f / 100 ml", v)
	}
	return fmt.Sprintf("%.1f %s / 100 ml", v, label)
}
