package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/ent/useridentity"
	"sodapop/internal/db"
)

const oidcStateCookie = "sd_oidc_state"

// OIDC wraps discovery, the OAuth2 config and the ID token verifier.
type OIDC struct {
	Enabled  bool
	Issuer   string
	Provider *oidc.Provider
	Verifier *oidc.IDTokenVerifier
	OAuth    *oauth2.Config
	stateKey []byte
}

type oidcState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Next     string `json:"next"`
}

type oidcClaims struct {
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Groups            []string `json:"groups"`
}

// InitOIDC discovers the provider when OIDC is enabled.
func (s *Service) InitOIDC(ctx context.Context) error {
	if !s.Cfg.OIDCEnabled {
		s.OIDC = &OIDC{Enabled: false}
		return nil
	}
	provider, err := oidc.NewProvider(ctx, s.Cfg.OIDCIssuer)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	o := &oauth2.Config{
		ClientID:     s.Cfg.OIDCClientID,
		ClientSecret: s.Cfg.OIDCClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.Cfg.OIDCRedirectURL,
		Scopes:       s.Cfg.OIDCScopes,
	}
	// Authelia accepts client_secret_post and client_secret_basic; go-oauth2
	// auto-detects on first use when AuthStyle is unset.
	s.OIDC = &OIDC{
		Enabled:  true,
		Issuer:   s.Cfg.OIDCIssuer,
		Provider: provider,
		Verifier: provider.Verifier(&oidc.Config{ClientID: s.Cfg.OIDCClientID}),
		OAuth:    o,
		stateKey: s.Cfg.SessionKey,
	}
	return nil
}

// BeginLogin stores signed state/nonce/PKCE data and redirects to the provider.
func (s *Service) BeginLogin(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil || !s.OIDC.Enabled {
		http.Error(w, "OIDC is not enabled", http.StatusNotFound)
		return
	}
	state := oidcState{
		State:    RandomToken(16),
		Nonce:    RandomToken(16),
		Verifier: RandomToken(32),
		Next:     safeNext(r.URL.Query().Get("next")),
	}
	payload, _ := json.Marshal(state)
	cookieVal := s.sign(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    cookieVal,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	url := s.OIDC.OAuth.AuthCodeURL(state.State,
		oauth2.SetAuthURLParam("nonce", state.Nonce),
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge(state.Verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, url, http.StatusFound)
}

// CompleteLogin handles the OIDC callback and establishes a session.
func (s *Service) CompleteLogin(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if s.OIDC == nil || !s.OIDC.Enabled {
		return errors.New("oidc is not enabled")
	}
	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		return errors.New("missing oidc state cookie")
	}
	payload, err := s.verifySigned(cookie.Value)
	if err != nil {
		return errors.New("invalid oidc state cookie")
	}
	var st oidcState
	if err := json.Unmarshal(payload, &st); err != nil {
		return err
	}
	if r.URL.Query().Get("state") != st.State {
		return errors.New("oidc state mismatch")
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})

	code := r.URL.Query().Get("code")
	if code == "" {
		return errors.New("missing authorization code")
	}
	token, err := s.OIDC.OAuth.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return fmt.Errorf("oidc exchange: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return errors.New("provider returned no id_token")
	}
	idToken, err := s.OIDC.Verifier.Verify(ctx, rawID)
	if err != nil {
		return fmt.Errorf("oidc verify: %w", err)
	}
	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		return err
	}
	if !claims.EmailVerified {
		return errors.New("provider has not verified this email address")
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return errors.New("provider returned no email")
	}
	if !s.Cfg.EmailAllowed(email) {
		return errors.New("this email address is not allowed")
	}

	u, err := s.linkOIDCUser(ctx, idToken.Subject, email, claims)
	if err != nil {
		return err
	}
	res, err := s.CreateSession(ctx, u.ID, clientIP(r), r.UserAgent())
	if err != nil {
		return err
	}
	s.SetSessionCookie(w, r, res.Token, res.ExpiresAt)

	next := st.Next
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusFound)
	return nil
}

func (s *Service) linkOIDCUser(ctx context.Context, subject, email string, claims oidcClaims) (*ent.User, error) {
	groupsJSON, _ := json.Marshal(claims.Groups)
	ident, err := s.Client.UserIdentity.Query().
		Where(useridentity.Provider("oidc"), useridentity.Subject(subject)).
		WithUser().
		Only(ctx)
	if err == nil && ident.Edges.User != nil {
		u := ident.Edges.User
		u, err = u.Update().
			SetEmailVerifiedAt(db.Now()).
			SetUpdatedAt(db.Now()).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		_, _ = ident.Update().SetEmail(email).SetGroupsJSON(string(groupsJSON)).SetUpdatedAt(db.Now()).Save(ctx)
		return u, nil
	}

	u, err := s.Client.User.Query().Where(user.Email(email), user.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return nil, err
		}
		u, err = s.createOIDCUser(ctx, email, claims)
		if err != nil {
			return nil, err
		}
	}

	_, err = s.Client.UserIdentity.Create().
		SetProvider("oidc").
		SetSubject(subject).
		SetUserID(u.ID).
		SetEmail(email).
		SetGroupsJSON(string(groupsJSON)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if u.EmailVerifiedAt == nil {
		u, err = u.Update().SetEmailVerifiedAt(db.Now()).SetUpdatedAt(db.Now()).Save(ctx)
		if err != nil {
			return nil, err
		}
	}
	role := u.Role
	for _, g := range claims.Groups {
		if g == "admins" || g == "admin" {
			role = "admin"
		}
	}
	if role != u.Role {
		u, err = u.Update().SetRole(user.Role(role)).SetUpdatedAt(db.Now()).Save(ctx)
		if err != nil {
			return nil, err
		}
	}
	return u, nil
}

func (s *Service) createOIDCUser(ctx context.Context, email string, claims oidcClaims) (*ent.User, error) {
	handle := sanitizeHandle(claims.PreferredUsername)
	if handle == "" {
		handle = sanitizeHandle(strings.Split(email, "@")[0])
	}
	if handle == "" {
		handle = "soda" + RandomToken(3)
	}
	handle = s.uniqueHandle(ctx, handle)
	display := strings.TrimSpace(claims.Name)
	if display == "" {
		display = handle
	}
	count, err := s.Client.User.Query().Count(ctx)
	if err != nil {
		return nil, err
	}
	role := "user"
	if count == 0 {
		role = "admin"
	}
	return s.Client.User.Create().
		SetHandle(handle).
		SetEmail(email).
		SetDisplayName(display).
		SetEmailVerifiedAt(db.Now()).
		SetRole(user.Role(role)).
		Save(ctx)
}

func (s *Service) uniqueHandle(ctx context.Context, base string) string {
	handle := base
	for i := 0; i < 50; i++ {
		n, err := s.Client.User.Query().Where(user.HandleEqualFold(handle)).Count(ctx)
		if err == nil && n == 0 {
			return handle
		}
		handle = fmt.Sprintf("%s%d", base, i+1)
	}
	return base + RandomToken(3)
}

// OIDCStatus exposes only non-sensitive discovery information.
func (s *Service) OIDCStatus() map[string]any {
	if s.OIDC == nil || !s.OIDC.Enabled {
		return map[string]any{"enabled": false, "password_fallback": s.Cfg.PasswordFallback}
	}
	return map[string]any{
		"enabled":           true,
		"issuer":            s.OIDC.Issuer,
		"password_fallback": s.Cfg.PasswordFallback,
	}
}

func (s *Service) sign(payload []byte) string {
	mac := hmac.New(sha256.New, s.Cfg.SessionKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) verifySigned(value string) ([]byte, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return nil, errors.New("malformed signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, s.Cfg.SessionKey)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("bad signature")
	}
	return payload, nil
}

func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/"
}

func sanitizeHandle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == '-' || r == '.' || r == ' ':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	return r.RemoteAddr
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
