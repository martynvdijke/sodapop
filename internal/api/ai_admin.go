package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"sodapop/ent"
	"sodapop/internal/ai"
	"sodapop/internal/auth"
)

func (s *Server) registerAIAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/ai-endpoints", s.protect("read", false, s.adminAIEndpointsList))
	mux.HandleFunc("POST /api/v1/admin/ai-endpoints", s.protectStrict("write", true, s.adminAIEndpointsCreate))
	mux.HandleFunc("PUT /api/v1/admin/ai-endpoints/{id}", s.protectStrict("write", true, s.adminAIEndpointsUpdate))
	mux.HandleFunc("DELETE /api/v1/admin/ai-endpoints/{id}", s.protect("write", true, s.adminAIEndpointsDelete))
	mux.HandleFunc("POST /api/v1/admin/ai-endpoints/{id}/toggle", s.protectStrict("write", true, s.adminAIEndpointsToggle))
	mux.HandleFunc("POST /api/v1/admin/ai-endpoints/{id}/test", s.protectStrict("write", true, s.adminAIEndpointsTest))
}

type aiEndpointBody struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	ClearAPIKey bool     `json:"clear_api_key"`
	Model       string   `json:"model"`
	Tags        []string `json:"tags"`
	Enabled     *bool    `json:"enabled"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"max_tokens"`
	ImageSize   string   `json:"image_size"`
}

func (s *Server) adminAIEndpointsList(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !adminAI(w, tok) {
		return nil
	}
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return nil
	}
	rows, err := s.AI.ListEndpoints(r.Context())
	if err != nil {
		s.internal(w, err)
		return nil
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, aiEndpointDTO(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

func (s *Server) adminAIEndpointsCreate(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !adminAI(w, tok) {
		return nil
	}
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return nil
	}
	var in aiEndpointBody
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	row, err := s.AI.SaveEndpoint(r.Context(), 0, endpointInput(in))
	if err != nil {
		s.writeAIAdminError(w, err)
		return nil
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": aiEndpointDTO(row)})
	return nil
}

func (s *Server) adminAIEndpointsUpdate(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !adminAI(w, tok) {
		return nil
	}
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return nil
	}
	id, ok := endpointID(w, r)
	if !ok {
		return nil
	}
	var in aiEndpointBody
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	row, err := s.AI.SaveEndpoint(r.Context(), id, endpointInput(in))
	if err != nil {
		s.writeAIAdminError(w, err)
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": aiEndpointDTO(row)})
	return nil
}

func (s *Server) adminAIEndpointsDelete(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !adminAI(w, tok) {
		return nil
	}
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return nil
	}
	id, ok := endpointID(w, r)
	if !ok {
		return nil
	}
	if err := s.AI.DeleteEndpoint(r.Context(), id); err != nil {
		s.writeAIAdminError(w, err)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) adminAIEndpointsToggle(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !adminAI(w, tok) {
		return nil
	}
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return nil
	}
	id, ok := endpointID(w, r)
	if !ok {
		return nil
	}
	rows, err := s.AI.ListEndpoints(r.Context())
	if err != nil {
		s.internal(w, err)
		return nil
	}
	var target *ent.AIEndpoint
	for _, row := range rows {
		if row.ID == id {
			target = row
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	enabled := !target.Enabled
	updated, err := s.AI.SaveEndpoint(r.Context(), id, ai.EndpointInput{
		Name:        target.Name,
		Type:        string(target.Type),
		BaseURL:     target.BaseURL,
		Model:       target.Model,
		Tags:        endpointTags(target.Tags),
		Enabled:     &enabled,
		Temperature: target.Temperature,
		MaxTokens:   target.MaxTokens,
		ImageSize:   target.ImageSize,
	})
	if err != nil {
		s.writeAIAdminError(w, err)
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": aiEndpointDTO(updated)})
	return nil
}

func (s *Server) adminAIEndpointsTest(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !adminAI(w, tok) {
		return nil
	}
	if s.AI == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI features are not configured")
		return nil
	}
	id, ok := endpointID(w, r)
	if !ok {
		return nil
	}
	status, preview, err := s.AI.TestEndpoint(r.Context(), id)
	if err != nil {
		if errors.Is(err, ai.ErrEndpointNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
		writeError(w, http.StatusBadGateway, "ai_unavailable", "the AI endpoint could not be reached")
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"status":  status,
		"preview": preview,
		"ok":      status >= 200 && status < 300,
	}})
	return nil
}

func endpointInput(in aiEndpointBody) ai.EndpointInput {
	return ai.EndpointInput{
		Name:        in.Name,
		Type:        in.Type,
		BaseURL:     in.BaseURL,
		APIKey:      in.APIKey,
		ClearAPIKey: in.ClearAPIKey,
		Model:       in.Model,
		Tags:        in.Tags,
		Enabled:     in.Enabled,
		Temperature: in.Temperature,
		MaxTokens:   in.MaxTokens,
		ImageSize:   in.ImageSize,
	}
}

func aiEndpointDTO(row *ent.AIEndpoint) map[string]any {
	if row == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":          row.ID,
		"name":        row.Name,
		"type":        row.Type,
		"base_url":    row.BaseURL,
		"api_key":     row.EncryptedAPIKey, // already masked by ListEndpoints/SaveEndpoint
		"model":       row.Model,
		"tags":        endpointTags(row.Tags),
		"enabled":     row.Enabled,
		"temperature": row.Temperature,
		"max_tokens":  row.MaxTokens,
		"image_size":  row.ImageSize,
		"created_at":  row.CreatedAt,
		"updated_at":  row.UpdatedAt,
	}
}

func endpointTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err == nil {
		return tags
	}
	out := []string{}
	for _, tag := range strings.Split(raw, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func endpointID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return 0, false
	}
	return id, true
}

func adminAI(w http.ResponseWriter, tok *auth.APIToken) bool {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return false
	}
	return true
}

func (s *Server) writeAIAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ai.ErrEndpointNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, ai.ErrInvalidInput), errors.Is(err, ai.ErrInvalidBaseURL):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ai.ErrDuplicateName):
		writeError(w, http.StatusConflict, "duplicate_name", "an AI endpoint with that name already exists")
	default:
		s.internal(w, err)
	}
}
