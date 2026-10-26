package api

import (
	"net/http"
	"strconv"

	"sodapop/internal/auth"
	"sodapop/internal/mix"
)

// myMixes returns the recipes authored by the viewer, newest first.
func (s *Server) myMixes(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	rows, err := s.Mix.Mine(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, mixDTO(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// getMix reads one mix by numeric id or slug, scoped to the viewer.
func (s *Server) getMix(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	ref := r.PathValue("id")
	var (
		m   *mix.Mix
		err error
	)
	if id, convErr := strconv.Atoi(ref); convErr == nil {
		m, err = s.Mix.GetForViewerByID(r.Context(), id, tok.User.ID)
	} else {
		m, err = s.Mix.GetForViewer(r.Context(), ref, tok.User.ID)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": mixDTO(*m)})
	return nil
}

// createMix publishes a recipe of 2..6 catalog drinks. Components accept either
// a pour volume in millilitres or an explicit part count.
func (s *Server) createMix(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Name       string `json:"name"`
		Notes      string `json:"notes"`
		Visibility string `json:"visibility"`
		Components []struct {
			DrinkID  int `json:"drink_id"`
			Parts    int `json:"parts"`
			VolumeML int `json:"volume_ml"`
		} `json:"components"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	components := make([]mix.Component, 0, len(in.Components))
	for _, c := range in.Components {
		components = append(components, mix.Component{
			DrinkID: c.DrinkID, Parts: c.Parts, VolumeML: c.VolumeML,
		})
	}
	m, err := s.Mix.Create(r.Context(), mix.Input{
		UserID: tok.User.ID, Name: in.Name, Notes: in.Notes,
		Visibility: in.Visibility, Components: components,
	})
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/mix/"+strconv.Itoa(m.ID))
	writeJSON(w, http.StatusCreated, map[string]any{"data": mixDTO(*m)})
	return nil
}

func mixDTO(m mix.Mix) map[string]any {
	components := make([]map[string]any, 0, len(m.Components))
	for _, c := range m.Components {
		components = append(components, map[string]any{
			"drink_id": c.DrinkID, "slug": c.Slug, "name": c.Name,
			"parts": c.Parts, "volume_ml": c.VolumeML,
		})
	}
	return map[string]any{
		"id": m.ID, "slug": m.Slug, "name": m.Name, "notes": m.Notes,
		"visibility": m.Visibility, "created_by": m.CreatedBy,
		"created_at": m.CreatedAt, "components": components,
	}
}
