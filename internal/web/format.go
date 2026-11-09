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

// BeverageTypeLabel humanises the catalog beverage_type enum for display.
func BeverageTypeLabel(v any) string {
	switch fmt.Sprint(v) {
	case "soda":
		return "Soda"
	case "water":
		return "Water"
	case "tea":
		return "Tea"
	case "coffee":
		return "Coffee"
	case "na_beer":
		return "NA beer"
	case "na_wine":
		return "NA wine"
	case "na_spirit":
		return "NA spirits"
	case "other":
		return "Other"
	default:
		return "Soda"
	}
}

// PrepMethodLabel humanises the check-in prep_method enum. "none" renders as
// an empty string so templates can treat an unset method as absent.
func PrepMethodLabel(v any) string {
	switch fmt.Sprint(v) {
	case "tea_bag":
		return "Tea bag"
	case "loose_leaf":
		return "Loose leaf"
	case "cold_brew":
		return "Cold brew"
	case "matcha":
		return "Matcha"
	case "espresso":
		return "Espresso"
	case "pour_over":
		return "Pour-over"
	case "french_press":
		return "French press"
	case "aeropress":
		return "AeroPress"
	case "moka":
		return "Moka pot"
	case "drip":
		return "Drip"
	case "other":
		return "Other"
	case "none", "":
		return ""
	default:
		return ""
	}
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
