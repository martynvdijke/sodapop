// Package integrations delivers Sodapop activity to per-user outbound
// integrations (Home Assistant, Matrix, TRMNL feeds) and browser push
// subscriptions. Everything here is best-effort: a failed delivery is logged
// and never blocks the request that produced the event.
package integrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/integration"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/settings"
)

// Event kinds emitted by the rest of the application.
const (
	EventCheckin = "checkin"
	EventToast   = "toast"
	EventFriend  = "friend"
	EventBadge   = "badge"
)

// Event is one user-visible activity to fan out.
type Event struct {
	UserID string
	Kind   string
	Text   string
	URL    string
}

// Service owns outbound integrations, push subscriptions, feeds and personal
// data export/import.
type Service struct {
	Client   *ent.Client
	SQL      *sql.DB
	Cfg      *config.Config
	HTTP     *http.Client
	Log      *slog.Logger
	Settings *settings.Service
}

// New wires the service to the shared database, configuration and logger.
func New(database *db.DB, cfg *config.Config, log *slog.Logger, runtime ...*settings.Service) *Service {
	s := &Service{
		Client: database.Client,
		SQL:    database.SQL,
		Cfg:    cfg,
		HTTP:   &http.Client{Timeout: 10 * time.Second},
		Log:    log,
	}
	if len(runtime) > 0 {
		s.Settings = runtime[0]
	}
	return s
}

// Save creates or updates the integration for a user and kind.
func (s *Service) Save(ctx context.Context, userID, kind, target, secret string, enabled bool) error {
	k, err := parseKind(kind)
	if err != nil {
		return err
	}
	existing, err := s.Client.Integration.Query().
		Where(integration.UserID(userID), integration.KindEQ(k)).
		Only(ctx)
	switch {
	case err == nil:
		_, err = existing.Update().
			SetTarget(target).
			SetSecret(secret).
			SetEnabled(enabled).
			SetUpdatedAt(db.Now()).
			Save(ctx)
		return err
	case ent.IsNotFound(err):
		_, err = s.Client.Integration.Create().
			SetUserID(userID).
			SetKind(k).
			SetTarget(target).
			SetSecret(secret).
			SetEnabled(enabled).
			SetCreatedAt(db.Now()).
			SetUpdatedAt(db.Now()).
			Save(ctx)
		return err
	default:
		return err
	}
}

// Get loads one integration for a user and kind.
func (s *Service) Get(ctx context.Context, userID, kind string) (*ent.Integration, error) {
	k, err := parseKind(kind)
	if err != nil {
		return nil, err
	}
	return s.Client.Integration.Query().
		Where(integration.UserID(userID), integration.KindEQ(k)).
		Only(ctx)
}

// List returns every integration configured by a user.
func (s *Service) List(ctx context.Context, userID string) ([]*ent.Integration, error) {
	return s.Client.Integration.Query().
		Where(integration.UserID(userID)).
		Order(ent.Asc(integration.FieldKind)).
		All(ctx)
}

func (s *Service) logger() *slog.Logger {
	if s != nil && s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// BaseURL returns the effective public base URL without a trailing slash.
func (s *Service) BaseURL() string { return s.baseURL() }

func (s *Service) baseURL() string {
	if s != nil && s.Settings != nil {
		if snap := s.Settings.Snapshot(); snap != nil {
			return snap.BaseURL()
		}
	}
	if s == nil || s.Cfg == nil {
		return ""
	}
	return strings.TrimRight(s.Cfg.BaseURL, "/")
}

func (s *Service) httpClient() *http.Client {
	if s != nil && s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func parseKind(kind string) (integration.Kind, error) {
	switch integration.Kind(strings.TrimSpace(kind)) {
	case integration.KindHomeAssistant:
		return integration.KindHomeAssistant, nil
	case integration.KindMatrix:
		return integration.KindMatrix, nil
	case integration.KindTrmnl:
		return integration.KindTrmnl, nil
	default:
		return "", fmt.Errorf("unknown integration kind %q", kind)
	}
}

func eventTitle(kind string) string {
	switch kind {
	case EventCheckin:
		return "New check-in"
	case EventToast:
		return "New toast"
	case EventFriend:
		return "New friend"
	case EventBadge:
		return "Badge unlocked"
	default:
		return "Sodapop"
	}
}

func errNoTarget() error {
	return errors.New("integration target is not configured")
}
