package api

import (
	"net/http"

	"sodapop/internal/auth"
)

func (s *Server) aiDraft(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Notes     string `json:"notes"`
		Barcode   string `json:"barcode"`
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	draft, reply, session, err := s.AI.DraftDrink(r.Context(), in.Notes, in.Barcode, in.SessionID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"draft": draft, "message": reply, "session_id": session,
	}})
	return nil
}

func (s *Server) aiTastingNote(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	note, err := s.AI.TastingNote(r.Context(), in.Text)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"note": note}})
	return nil
}

func (s *Server) aiRecommend(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		History string `json:"history"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	text, err := s.AI.Recommend(r.Context(), in.History)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"recommendation": text}})
	return nil
}
