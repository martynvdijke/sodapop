// Package settings resolves admin-editable runtime settings from built-in
// defaults, environment configuration and database overrides, and publishes an
// immutable snapshot for lock-free reads.
package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"sodapop/ent"
	"sodapop/ent/adminnote"
	"sodapop/ent/runtimesetting"
	"sodapop/internal/config"
	"sodapop/internal/secretbox"
)

// Service owns the runtime settings snapshot and the write path.
type Service struct {
	client *ent.Client
	cfg    *config.Config
	box    *secretbox.Box
	log    *slog.Logger
	snap   atomic.Pointer[Snapshot]
}

// New builds the settings service. secret is the raw SESSION_SECRET; when it
// is empty the already-resolved config key is used. The initial snapshot
// contains defaults and environment values; call Load to include DB overrides.
func New(client *ent.Client, cfg *config.Config, secret string, log *slog.Logger) (*Service, error) {
	if log == nil {
		log = slog.Default()
	}
	if secret == "" && cfg != nil {
		secret = string(cfg.SessionKey)
	}
	box, err := secretbox.New(secret, secretbox.DomainSettings)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	s := &Service{client: client, cfg: cfg, box: box, log: log}
	s.snap.Store(buildSnapshot(cfg, nil, box, log))
	return s, nil
}

// Snapshot returns the current immutable snapshot. The pointer may be retained;
// later writes swap in a new snapshot without mutating this one.
func (s *Service) Snapshot() *Snapshot {
	if s == nil {
		return nil
	}
	return s.snap.Load()
}

// Load reads every runtime_settings row, merges it over env/default values and
// atomically publishes the result. A query failure leaves the previous
// snapshot untouched.
func (s *Service) Load(ctx context.Context) error {
	if s == nil {
		return errors.New("settings: nil service")
	}
	var rows []*ent.RuntimeSetting
	if s.client != nil {
		var err error
		rows, err = s.client.RuntimeSetting.Query().
			Order(ent.Asc(runtimesetting.FieldID)).
			All(ctx)
		if err != nil {
			return fmt.Errorf("settings: load: %w", err)
		}
	}
	s.snap.Store(buildSnapshot(s.cfg, rows, s.box, s.log))
	return nil
}

// Set validates and persists one runtime setting, then publishes a fresh
// snapshot. A blank secret keeps the stored value; use Delete to clear it.
// Secrets are encrypted before the row is written and never logged.
func (s *Service) Set(ctx context.Context, key, value string, secret bool, updatedBy string) error {
	if s == nil {
		return errors.New("settings: nil service")
	}
	spec, ok := specs[key]
	if !ok {
		if isEnvOnlyKey(key) {
			return fmt.Errorf("%w: %s", ErrEnvOnlyKey, key)
		}
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if secret && !spec.secret {
		return fmt.Errorf("%w: %s", ErrSecretKey, key)
	}
	isSecret := spec.secret || secret

	var storedValue, encrypted string
	if isSecret {
		if strings.TrimSpace(value) == "" {
			// Blank secret input preserves the existing secret.
			return nil
		}
		var err error
		encrypted, err = s.box.Encrypt(value)
		if err != nil {
			return fmt.Errorf("settings: encrypt %s: %w", key, err)
		}
	} else {
		normalized, err := normalizeValue(key, value)
		if err != nil {
			return err
		}
		storedValue = normalized
	}

	if s.client == nil {
		return errors.New("settings: no database")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("settings: begin: %w", err)
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}

	now := time.Now().UTC().Format(time.RFC3339)
	existing, err := tx.RuntimeSetting.Query().
		Where(runtimesetting.KeyEQ(key)).
		Only(ctx)
	switch {
	case err == nil:
		update := tx.RuntimeSetting.UpdateOneID(existing.ID).
			SetValue(storedValue).
			SetEncryptedValue(encrypted).
			SetIsSecret(isSecret).
			SetUpdatedAt(now)
		if updatedBy != "" {
			update.SetUpdatedBy(updatedBy)
		} else {
			update.ClearUpdatedBy()
		}
		if _, err := update.Save(ctx); err != nil {
			return rollback(fmt.Errorf("settings: update %s: %w", key, err))
		}
	case ent.IsNotFound(err):
		create := tx.RuntimeSetting.Create().
			SetKey(key).
			SetValue(storedValue).
			SetEncryptedValue(encrypted).
			SetIsSecret(isSecret).
			SetUpdatedAt(now)
		if updatedBy != "" {
			create.SetUpdatedBy(updatedBy)
		}
		if _, err := create.Save(ctx); err != nil {
			return rollback(fmt.Errorf("settings: create %s: %w", key, err))
		}
	default:
		return rollback(fmt.Errorf("settings: read %s: %w", key, err))
	}

	if err := appendAudit(ctx, tx.Client(), updatedBy, auditBody(key, "updated")); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("settings: commit %s: %w", key, err)
	}
	if err := s.Load(ctx); err != nil {
		// The write committed; the caller needs to know the snapshot is stale.
		return err
	}
	return nil
}

// Delete removes the database override for key, reverting to env/default, and
// publishes a fresh snapshot. Deleting a missing row is a no-op.
func (s *Service) Delete(ctx context.Context, key, updatedBy string) error {
	if s == nil {
		return errors.New("settings: nil service")
	}
	if _, ok := specs[key]; !ok {
		if isEnvOnlyKey(key) {
			return fmt.Errorf("%w: %s", ErrEnvOnlyKey, key)
		}
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if s.client == nil {
		return errors.New("settings: no database")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("settings: begin: %w", err)
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}

	deleted, err := tx.RuntimeSetting.Delete().
		Where(runtimesetting.KeyEQ(key)).
		Exec(ctx)
	if err != nil {
		return rollback(fmt.Errorf("settings: delete %s: %w", key, err))
	}
	if deleted > 0 {
		if err := appendAudit(ctx, tx.Client(), updatedBy, auditBody(key, "cleared")); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("settings: commit delete %s: %w", key, err)
	}
	if deleted == 0 {
		return nil
	}
	return s.Load(ctx)
}

// appendAudit records a key name only. Values, plaintext and ciphertext never
// enter the audit log.
func appendAudit(ctx context.Context, client *ent.Client, author, body string) error {
	if author == "" {
		return nil
	}
	if _, err := client.AdminNote.Create().
		SetAuthorID(author).
		SetEntity(adminnote.EntitySystem).
		SetEntityID(0).
		SetBody(body).
		SetCreatedAt(time.Now().UTC().Format(time.RFC3339)).
		Save(ctx); err != nil {
		return fmt.Errorf("settings: audit: %w", err)
	}
	return nil
}

func auditBody(key, action string) string {
	return "settings." + key + ": " + action
}
