package api

import (
	"net/http"

	"sodapop/ent"
	"sodapop/internal/auth"
)

// integrationHAStatus reports the caller's Home Assistant wiring without ever
// echoing a token or a shared secret. Inbound tokens are listed by metadata
// only; the plaintext is returned once by the web UI when it is created.
func (s *Server) integrationHAStatus(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	ctx := r.Context()
	enabled := false
	target := ""
	row, err := s.Integrations.Get(ctx, tok.User.ID, "home_assistant")
	switch {
	case err == nil:
		enabled = row.Enabled
		target = row.Target
	case ent.IsNotFound(err):
	default:
		return err
	}

	tokens := []map[string]any{}
	active := 0
	if rows, err := s.Integrations.ListInboundTokens(ctx, tok.User.ID); err == nil {
		for _, row := range rows {
			revoked := row.RevokedAt != nil
			if !revoked {
				active++
			}
			lastUsed := ""
			if row.LastUsedAt != nil {
				lastUsed = *row.LastUsedAt
			}
			tokens = append(tokens, map[string]any{
				"id":           row.ID,
				"label":        row.Label,
				"created_at":   row.CreatedAt,
				"last_used_at": lastUsed,
				"revoked":      revoked,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"outbound": map[string]any{
			"kind":    "home_assistant",
			"enabled": enabled,
			"target":  target,
		},
		"inbound": map[string]any{
			"webhook_path":  "/hooks/ha/{token}",
			"active_tokens": active,
			"tokens":        tokens,
		},
	}})
	return nil
}
