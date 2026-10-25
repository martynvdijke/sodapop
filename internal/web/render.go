package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"sodapop/ent"
)

//go:embed templates/*.html templates/pages/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Renderer struct {
	pages map[string]*template.Template
}

func NewRenderer() (*Renderer, error) {
	names := []string{"home", "login", "register", "verify", "search", "drink", "checkin", "friends", "tokens", "newdrink", "error"}
	r := &Renderer{pages: map[string]*template.Template{}}
	for _, name := range names {
		t, err := template.New("base").Funcs(template.FuncMap{
			"rating":    ratingLabel,
			"stars":     starsValue,
			"deref":     deref,
			"initial":   initial,
			"percent":   percent,
			"container": func(v any) string { return titleCase(fmt.Sprint(v)) },
			"hasPrefix": strings.HasPrefix,
		}).ParseFS(templateFS,
			"templates/base.html",
			"templates/partials.html",
			"templates/pages/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

// Render executes a page template into a buffer before writing, so a template
// error never produces a half-written page.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data map[string]any) {
	t, ok := r.pages[page]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base.html", data); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func ratingLabel(v *int) string {
	if v == nil {
		return "Not rated"
	}
	whole := *v / 4
	quarter := *v % 4
	switch quarter {
	case 0:
		return fmt.Sprintf("%d.0", whole)
	case 1:
		return fmt.Sprintf("%d.25", whole)
	case 2:
		return fmt.Sprintf("%d.5", whole)
	default:
		return fmt.Sprintf("%d.75", whole)
	}
}

func starsLabel(v *int) string {
	if v == nil {
		return "Not rated"
	}
	return ratingLabel(v) + " / 5"
}

// starsValue accepts both *int quarter-star ratings and *float64 averages so
// templates can share one formatter.
func starsValue(v any) string {
	switch p := v.(type) {
	case nil:
		return "Not rated"
	case *int:
		if p == nil {
			return "Not rated"
		}
		return ratingLabel(p) + " / 5"
	case *float64:
		if p == nil {
			return "Not rated"
		}
		return fmt.Sprintf("%.2f / 5", *p)
	case int:
		return ratingLabel(&p) + " / 5"
	case float64:
		return fmt.Sprintf("%.2f / 5", p)
	default:
		return "Not rated"
	}
}

func deref(v any) any {
	switch p := v.(type) {
	case *float64:
		if p != nil {
			return *p
		}
	case *int:
		if p != nil {
			return *p
		}
	case *string:
		if p != nil {
			return *p
		}
	}
	return ""
}

func initial(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(s)[0]))
}

func percent(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}

// UserFromContext returns the session user attached by the web middleware.
func UserFromContext(r *http.Request) *ent.User {
	if u, ok := r.Context().Value(userKey{}).(*ent.User); ok {
		return u
	}
	return nil
}

// SessionFromContext returns the session row attached by the web middleware.
func SessionFromContext(r *http.Request) *ent.Session {
	if s, ok := r.Context().Value(sessionKey{}).(*ent.Session); ok {
		return s
	}
	return nil
}
