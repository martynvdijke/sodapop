package web

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"testing"
)

func TestFormatVolume(t *testing.T) {
	tests := []struct {
		name  string
		ml    int
		units string
		want  string
	}{
		{"metric bottle", 355, "metric", "355 ml"},
		{"default units are metric", 330, "", "330 ml"},
		{"imperial can", 355, "imperial", "12.0 fl oz"},
		{"imperial half litre", 500, "imperial", "16.9 fl oz"},
		{"zero is still explicit", 0, "metric", "0 ml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatVolume(tt.ml, tt.units); got != tt.want {
				t.Fatalf("FormatVolume(%d, %q) = %q, want %q", tt.ml, tt.units, got, tt.want)
			}
		})
	}
}

func TestFormatPer100(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		units string
		want  string
	}{
		{"metric", 10.5, "metric", "10.5 / 100 ml"},
		{"default units are metric", 4.2, "", "4.2 / 100 ml"},
		{"imperial converts to fluid ounce", 10, "imperial", "3.0 / fl oz"},
		{"imperial rounds one decimal", 9.5, "imperial", "2.8 / fl oz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatPer100(tt.value, tt.units); got != tt.want {
				t.Fatalf("FormatPer100(%v, %q) = %q, want %q", tt.value, tt.units, got, tt.want)
			}
		})
	}
}

func TestRenderPer100AcceptsTypedInt(t *testing.T) {
	// Ent stores caffeine as an int; text/template does not coerce a typed
	// field to float64, so this must execute rather than fall back to the
	// template-error page.
	tmpl := template.Must(template.New("base.html").Funcs(template.FuncMap{
		"per100": templatePer100,
	}).Parse(`{{per100 .Value "mg"}}`))
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", map[string]any{"Value": 20}); err != nil {
		t.Fatalf("execute per100 with int: %v", err)
	}
	if got, want := buf.String(), "20.0 mg / 100 ml"; got != want {
		t.Fatalf("per100(int) = %q, want %q", got, want)
	}
}

func TestRenderRebindsUnitsPerRequest(t *testing.T) {
	src := `{{volume 355}}|{{per100 10 "mg"}}`
	metric := template.Must(template.New("base.html").Funcs(metricFuncs()).Parse(src))
	imperial := template.Must(template.New("base.html").Funcs(imperialFuncs()).Parse(src))
	r := &Renderer{
		pages:         map[string]*template.Template{"units": metric},
		imperialPages: map[string]*template.Template{"units": imperial},
	}

	render := func(units string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		r.Render(rec, 200, "units", map[string]any{"Units": units})
		return rec.Body.String()
	}

	if got, want := render("metric"), "355 ml|10.0 mg / 100 ml"; got != want {
		t.Fatalf("metric render = %q, want %q", got, want)
	}
	if got, want := render("imperial"), "12.0 fl oz|3.0 mg / fl oz"; got != want {
		t.Fatalf("imperial render = %q, want %q", got, want)
	}
	if got, want := render("metric"), "355 ml|10.0 mg / 100 ml"; got != want {
		t.Fatalf("metric render after imperial = %q, want %q", got, want)
	}
}

// TestRenderImperialAfterMetricExecutes is the regression for cloning an
// already-executed template: the imperial render must still be correct after
// the metric template has run.
func TestRenderImperialAfterMetricExecutes(t *testing.T) {
	src := `{{volume 355}}|{{per100 10 "mg"}}`
	metric := template.Must(template.New("base.html").Funcs(metricFuncs()).Parse(src))
	imperial := template.Must(template.New("base.html").Funcs(imperialFuncs()).Parse(src))
	r := &Renderer{
		pages:         map[string]*template.Template{"units": metric},
		imperialPages: map[string]*template.Template{"units": imperial},
	}

	rec := httptest.NewRecorder()
	r.Render(rec, 200, "units", map[string]any{"Units": "metric"})
	if got, want := rec.Body.String(), "355 ml|10.0 mg / 100 ml"; got != want {
		t.Fatalf("first metric render = %q, want %q", got, want)
	}

	rec = httptest.NewRecorder()
	r.Render(rec, 200, "units", map[string]any{"Units": "imperial"})
	if got, want := rec.Body.String(), "12.0 fl oz|3.0 mg / fl oz"; got != want {
		t.Fatalf("imperial render after metric = %q, want %q", got, want)
	}

	rec = httptest.NewRecorder()
	r.Render(rec, 200, "units", map[string]any{"Units": "imperial"})
	if got, want := rec.Body.String(), "12.0 fl oz|3.0 mg / fl oz"; got != want {
		t.Fatalf("second imperial render = %q, want %q", got, want)
	}
}
