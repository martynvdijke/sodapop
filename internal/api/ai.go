package api

import (
	"net/http"

	"sodapop/internal/ai"
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
	if !s.aiReady(w, r) {
		return nil
	}
	session, err := s.AI.CreateDraftSession(r.Context(), tok.User.ID, "drink", in.Notes, in.Barcode)
	if err != nil {
		s.writeAIError(w, err)
		return nil
	}
	turn, err := ai.DraftTurnFromSession(session)
	if err != nil {
		s.writeAIError(w, err)
		return nil
	}
	// Keep the existing data.draft/message/session_id shape. session_id now
	// identifies the persisted session used by the follow-up routes.
	writeAITurn(w, turn)
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
