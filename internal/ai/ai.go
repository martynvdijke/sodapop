package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"sodapop/ent"
	"sodapop/ent/aiendpoint"
	"sodapop/ent/appsetting"
	"sodapop/internal/catalog"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

// Service owns AI endpoint configuration and outbound provider calls.
type Service struct {
	Client  *ent.Client
	Cfg     *config.Config
	HTTP    *http.Client
	Version string
	Log     *slog.Logger
	// Catalog commits drafts into provisional drinks. New populates it when a
	// database is available; callers may override it in tests.
	Catalog *catalog.Service
}

// New builds the AI service. database may be nil for env-only use; calls that
// need persisted endpoints then return ErrNoEndpoint unless AI_BASE_URL is set.
func New(database *db.DB, cfg *config.Config, version string, log *slog.Logger) *Service {
	s := &Service{
		Cfg:     cfg,
		HTTP:    &http.Client{Timeout: TextTimeout},
		Version: version,
		Log:     log,
	}
	if database != nil {
		s.Client = database.Client
		if cfg != nil {
			s.Catalog = catalog.New(cfg, database)
		}
	}
	if strings.TrimSpace(s.Version) == "" && cfg != nil {
		s.Version = cfg.Version
	}
	return s
}

// secret returns the material used for API-key encryption. The session secret
// is already mandatory in production, so no second secret has to be managed.
func (s *Service) secret() string {
	if s != nil && s.Cfg != nil && len(s.Cfg.SessionKey) > 0 {
		return string(s.Cfg.SessionKey)
	}
	return os.Getenv("SESSION_SECRET")
}

// Enabled reports whether AI features are globally switched on. With no
// ai_enabled row it is true only when env-only provider configuration exists.
func (s *Service) Enabled(ctx context.Context) (bool, error) {
	if s != nil && s.Client != nil {
		setting, err := s.Client.AppSetting.Query().
			Where(appsetting.KeyEQ("ai_enabled")).
			Only(ctx)
		switch {
		case err == nil:
			return setting.Value == "1", nil
		case !ent.IsNotFound(err):
			return false, err
		}
	}
	return envConfigured(), nil
}

func envConfigured() bool {
	return strings.TrimSpace(os.Getenv("AI_BASE_URL")) != ""
}

// ListEndpoints returns all endpoints for the admin UI with API keys masked.
func (s *Service) ListEndpoints(ctx context.Context) ([]*ent.AIEndpoint, error) {
	if s == nil || s.Client == nil {
		return []*ent.AIEndpoint{}, nil
	}
	rows, err := s.Client.AIEndpoint.Query().
		Order(ent.Asc(aiendpoint.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*ent.AIEndpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.maskedEndpoint(row))
	}
	return out, nil
}

// EndpointInput is the admin-editable endpoint shape. Pointer fields preserve
// existing values on update and use schema defaults on create.
type EndpointInput struct {
	Name        string
	Type        string
	BaseURL     string
	APIKey      string
	ClearAPIKey bool
	Model       string
	Tags        []string
	Enabled     *bool
	Temperature *float64
	MaxTokens   *int
	ImageSize   string
}

// SaveEndpoint creates an endpoint when id is 0, otherwise updates it. An empty
// APIKey leaves the stored key untouched unless ClearAPIKey is set. The
// returned entity has its key masked.
func (s *Service) SaveEndpoint(ctx context.Context, id int, in EndpointInput) (*ent.AIEndpoint, error) {
	if s == nil || s.Client == nil {
		return nil, errors.New("ai endpoint storage is not configured")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: endpoint name is required", ErrInvalidInput)
	}
	kind := strings.TrimSpace(in.Type)
	if kind == "" {
		if id > 0 {
			existing, err := s.Client.AIEndpoint.Get(ctx, id)
			if err != nil {
				if ent.IsNotFound(err) {
					return nil, ErrEndpointNotFound
				}
				return nil, err
			}
			kind = string(existing.Type)
		} else {
			kind = string(aiendpoint.TypeText)
		}
	}
	kindType := aiendpoint.Type(kind)
	if err := aiendpoint.TypeValidator(kindType); err != nil {
		return nil, fmt.Errorf("%w: type must be text, image or vision", ErrInvalidInput)
	}
	baseURL, err := NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", ErrInvalidInput)
	}
	if in.Temperature != nil && (*in.Temperature < 0 || *in.Temperature > 2) {
		return nil, fmt.Errorf("%w: temperature must be between 0 and 2", ErrInvalidInput)
	}
	if in.MaxTokens != nil && *in.MaxTokens <= 0 {
		return nil, fmt.Errorf("%w: max_tokens must be positive", ErrInvalidInput)
	}

	duplicate, err := s.Client.AIEndpoint.Query().
		Where(aiendpoint.NameEQ(name), aiendpoint.IDNEQ(id)).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if duplicate {
		return nil, ErrDuplicateName
	}

	tags := "[]"
	if in.Tags != nil {
		encoded, err := json.Marshal(in.Tags)
		if err != nil {
			return nil, fmt.Errorf("%w: tags must be strings", ErrInvalidInput)
		}
		tags = string(encoded)
	}

	if id == 0 {
		builder := s.Client.AIEndpoint.Create().
			SetName(name).
			SetType(kindType).
			SetBaseURL(baseURL).
			SetModel(model).
			SetTags(tags)
		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		builder.SetEnabled(enabled)
		if in.Temperature != nil {
			builder.SetTemperature(*in.Temperature)
		}
		if in.MaxTokens != nil {
			builder.SetMaxTokens(clamp(*in.MaxTokens, minMaxTokens, maxMaxTokens))
		}
		if in.ImageSize != "" {
			builder.SetImageSize(strings.TrimSpace(in.ImageSize))
		}
		if key := strings.TrimSpace(in.APIKey); key != "" {
			encrypted, err := encryptAPIKey(s.secret(), key)
			if err != nil {
				return nil, err
			}
			builder.SetEncryptedAPIKey(encrypted)
		}
		row, err := builder.Save(ctx)
		if err != nil {
			return nil, err
		}
		return s.maskedEndpoint(row), nil
	}

	update := s.Client.AIEndpoint.UpdateOneID(id).
		SetName(name).
		SetType(kindType).
		SetBaseURL(baseURL).
		SetModel(model).
		SetUpdatedAt(db.Now())
	if in.Tags != nil {
		update.SetTags(tags)
	}
	if in.Enabled != nil {
		update.SetEnabled(*in.Enabled)
	}
	if in.Temperature != nil {
		update.SetTemperature(*in.Temperature)
	}
	if in.MaxTokens != nil {
		update.SetMaxTokens(clamp(*in.MaxTokens, minMaxTokens, maxMaxTokens))
	}
	if in.ImageSize != "" {
		update.SetImageSize(strings.TrimSpace(in.ImageSize))
	}
	if in.ClearAPIKey {
		update.SetEncryptedAPIKey("")
	} else if key := strings.TrimSpace(in.APIKey); key != "" {
		encrypted, err := encryptAPIKey(s.secret(), key)
		if err != nil {
			return nil, err
		}
		update.SetEncryptedAPIKey(encrypted)
	}
	if err := update.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrEndpointNotFound
		}
		return nil, err
	}
	row, err := s.Client.AIEndpoint.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.maskedEndpoint(row), nil
}

// DeleteEndpoint removes an endpoint row.
func (s *Service) DeleteEndpoint(ctx context.Context, id int) error {
	if s == nil || s.Client == nil {
		return errors.New("ai endpoint storage is not configured")
	}
	if err := s.Client.AIEndpoint.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return ErrEndpointNotFound
		}
		return err
	}
	return nil
}

// TestEndpoint probes an endpoint with GET <base>/models. The returned status
// and 200-character preview are the diagnostic; err is reserved for local
// failures (missing row, unusable key, transport). An explicit id is tested
// even when the endpoint is disabled or AI is globally off.
func (s *Service) TestEndpoint(ctx context.Context, id int) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()

	var ep *endpoint
	var err error
	if id > 0 {
		if s == nil || s.Client == nil {
			return 0, "", ErrEndpointNotFound
		}
		row, getErr := s.Client.AIEndpoint.Get(ctx, id)
		if getErr != nil {
			if ent.IsNotFound(getErr) {
				return 0, "", ErrEndpointNotFound
			}
			return 0, "", getErr
		}
		ep, err = s.endpointFromRow(row)
	} else {
		ep, err = s.resolveEndpoint(ctx)
	}
	if err != nil {
		return 0, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.BaseURL+"/models", nil)
	if err != nil {
		return 0, "", fmt.Errorf("create ai test request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if ep.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+ep.APIKey)
	}
	setProviderHeaders(req, s.version(), GenerateSessionID())

	resp, err := s.clientFor(TestTimeout).Do(req)
	if err != nil {
		s.logger().Error("ai endpoint test failed", "endpoint_id", ep.ID, "error", err)
		return 0, "", fmt.Errorf("ai provider request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("read ai test response: %w", err)
	}
	return resp.StatusCode, truncateText(string(body), 200), nil
}

// maskedEndpoint copies an endpoint and replaces the stored ciphertext with a
// display-only mask. Entities are never mutated in place.
func (s *Service) maskedEndpoint(row *ent.AIEndpoint) *ent.AIEndpoint {
	if row == nil {
		return nil
	}
	out := *row
	if row.EncryptedAPIKey != "" {
		plaintext, err := decryptAPIKey(s.secret(), row.EncryptedAPIKey)
		if err != nil {
			out.EncryptedAPIKey = "***"
		} else {
			out.EncryptedAPIKey = MaskAPIKey(plaintext)
		}
	}
	return &out
}
