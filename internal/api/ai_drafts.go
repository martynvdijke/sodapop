package api

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"sodapop/internal/ai"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
)

// aiAPIMuxes makes RegisterAIAPI idempotent per mux. That lets the central
// router add the call without breaking test harnesses that also opt in.
var aiAPIMuxes sync.Map

// RegisterAIAPI wires the multi-turn draft and admin endpoint routes. It is
// split out so the central router can opt in without this lane editing it.
func (s *Server) RegisterAIAPI(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	if _, loaded := aiAPIMuxes.LoadOrStore(mux, true); loaded {
		return
	}
	s.registerAIDraftRoutes(mux)
	s.registerAIAdminRoutes(mux)
}

func (s *Server) registerAIDraftRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/ai/drafts/{id}/messages", s.protectStrict("write", true, s.aiDraftMessage))
	mux.HandleFunc("POST /api/v1/ai/drafts/{id}/revise", s.protectStrict("write", true, s.aiDraftRevise))
	mux.HandleFunc("POST /api/v1/ai/drafts/{id}/commit", s.protectStrict("write", true, s.aiDraftCommit))
}

func (s *Server) aiDraftMessage(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is required")
		return nil
	}
	if len([]rune(content)) > ai.MaxDraftMessageRunes {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is too long")
		return nil
	}
	if !s.aiReady(w, r) {
		return nil
	}
	turn, err := s.AI.AppendDraftMessage(r.Context(), tok.User.ID, r.PathValue("id"), content)
	if err != nil {
		s.writeAIError(w, err)
		return nil
	}
	writeAITurn(w, turn)
	return nil
}

func (s *Server) aiDraftRevise(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Instruction string `json:"instruction"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	instruction := strings.TrimSpace(in.Instruction)
	if instruction == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "instruction is required")
		return nil
	}
	if len([]rune(instruction)) > ai.MaxDraftMessageRunes {
		writeError(w, http.StatusBadRequest, "invalid_request", "instruction is too long")
		return nil
	}
	if !s.aiReady(w, r) {
		return nil
	}
	turn, err := s.AI.ReviseDraft(r.Context(), tok.User.ID, r.PathValue("id"), instruction)
	if err != nil {
		s.writeAIError(w, err)
		return nil
	}
	writeAITurn(w, turn)
	return nil
}

func (s *Server) aiDraftCommit(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.aiReady(w, r) {
		return nil
	}
	drink, err := s.AI.CommitDraft(r.Context(), tok.User.ID, r.PathValue("id"))
	if err != nil {
		s.writeAIError(w, err)
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"id":         drink.ID,
		"drink_id":   drink.ID,
		"slug":       drink.Slug,
		"name":       drink.Name,
		"status":     drink.Status,
		"session_id": r.PathValue("id"),
		"message":    "provisional drink committed",
	}})
	return nil
}

func writeAITurn(w http.ResponseWriter, turn *ai.DraftTurn) {
	if turn == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
		return
	}
	data := map[string]any{
		"session_id": turn.SessionID,
		"status":     turn.Status,
		"message":    turn.Message,
		"draft":      turn.Draft,
	}
	if turn.UpstreamSessionID != "" {
		data["upstream_session_id"] = turn.UpstreamSessionID
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// aiReady returns false after writing a service-unavailable response when the
// AI service is missing or globally disabled.
func (s *Server) aiReady(w http.ResponseWriter, r *http.Request) bool {
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return false
	}
	enabled, err := s.AI.Enabled(r.Context())
	if err != nil {
		s.internal(w, err)
		return false
	}
	if !enabled {
		writeError(w, http.StatusServiceUnavailable, "ai_disabled", "AI features are disabled")
		return false
	}
	return true
}

// writeAIError keeps the documented ai_disabled/ai_output_truncated codes and
// hides another user's sessions behind the same 404 as an unknown id.
func (s *Server) writeAIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ai.ErrDraftNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, ai.ErrDraftCommitted):
		writeError(w, http.StatusConflict, "draft_committed", "this draft has already been committed")
	case errors.Is(err, ai.ErrNotEnabled):
		writeError(w, http.StatusServiceUnavailable, "ai_disabled", "AI features are disabled")
	case errors.Is(err, ai.ErrTruncated):
		writeError(w, http.StatusBadGateway, "ai_output_truncated", "the AI response was cut off")
	case errors.Is(err, ai.ErrInvalidOutput):
		writeError(w, http.StatusBadGateway, "ai_invalid_response", "the AI returned an unusable response")
	case errors.Is(err, ai.ErrNoEndpoint):
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "no AI endpoint is configured")
	case errors.Is(err, ai.ErrDraftIncomplete):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ai.ErrDraftTooLarge):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ai.ErrNothingToSend), errors.Is(err, ai.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, catalog.ErrInvalidBarcode):
		writeError(w, http.StatusBadRequest, "invalid_barcode", "draft barcode is not a valid GTIN")
	case errors.Is(err, catalog.ErrBarcodeTaken):
		writeError(w, http.StatusConflict, "barcode_taken", "draft barcode already belongs to another drink")
	default:
		s.internal(w, err)
	}
}
