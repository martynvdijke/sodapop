package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"sodapop/ent/adminnote"
	"sodapop/ent/runtimesetting"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/secretbox"
)

func testConfig() *config.Config {
	return &config.Config{
		AppName:          defaultAppName,
		BaseURL:          defaultBaseURL,
		SMTPPort:         defaultSMTPPort,
		SMTPFrom:         defaultSMTPFrom,
		SMTPTLS:          defaultSMTPTLS,
		PasswordFallback: defaultAllowPasswordFallback,
		SessionKey:       []byte("test-session-secret"),
		SessionTTL:       time.Hour,
	}
}

func newTestService(t *testing.T, cfg *config.Config, secret string, log *slog.Logger) (*Service, *db.DB) {
	t.Helper()
	dsn := fmt.Sprintf("settings-%d?mode=memory&cache=shared&", time.Now().UnixNano())
	database, err := db.Init(dsn)
	if err != nil {
		t.Fatalf("init settings db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	s, err := New(database.Client, cfg, secret, log)
	if err != nil {
		t.Fatalf("new settings service: %v", err)
	}
	if err := s.Load(context.Background()); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return s, database
}

func TestRuntimeSettingTableExists(t *testing.T) {
	_, database := newTestService(t, testConfig(), "", nil)
	var n int
	if err := database.SQL.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'runtime_settings'`).Scan(&n); err != nil {
		t.Fatalf("inspect sqlite_master: %v", err)
	}
	if n != 1 {
		t.Fatalf("runtime_settings table count = %d, want 1", n)
	}
	var unique int
	if err := database.SQL.QueryRow(
		`SELECT count(*) FROM pragma_index_list('runtime_settings') WHERE "unique" = 1`).Scan(&unique); err != nil {
		t.Fatalf("inspect runtime_settings indexes: %v", err)
	}
	if unique == 0 {
		t.Fatal("runtime_settings.key has no unique index")
	}
}

func TestDefaultsAndPrecedence(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()
	s, _ := newTestService(t, cfg, "", nil)
	snap := s.Snapshot()

	if got := snap.SiteName(); got != defaultAppName {
		t.Fatalf("site name = %q, want %q", got, defaultAppName)
	}
	if got := snap.Source(KeyAppName); got != "default" {
		t.Fatalf("app_name source = %q, want default", got)
	}
	if got := snap.BaseURL(); got != defaultBaseURL {
		t.Fatalf("base url = %q, want %q", got, defaultBaseURL)
	}
	if got := snap.SMTP(); got.Port != defaultSMTPPort || got.From != defaultSMTPFrom || got.TLS != defaultSMTPTLS {
		t.Fatalf("smtp defaults = %+v", got)
	}
	if !snap.RegistrationOpen() {
		t.Fatal("registration should default to open")
	}
	if snap.AIEnabled() {
		t.Fatal("AI should default to disabled")
	}
	if !snap.AllowPasswordFallback() {
		t.Fatal("password fallback should default to enabled")
	}
	if got := snap.AllowedEmails(); got != nil {
		t.Fatalf("allowed emails = %v, want nil", got)
	}
	if got := snap.Source("not-a-key"); got != "" {
		t.Fatalf("unknown source = %q, want empty", got)
	}

	// Environment layer.
	cfg.AppName = "Env Pop"
	cfg.BaseURL = "https://env.example.test/"
	cfg.SMTPHost = "smtp.env.test"
	cfg.SMTPPort = 2525
	cfg.SMTPUser = "env-user"
	cfg.SMTPPass = "env-pass"
	cfg.SMTPFrom = "Env <env@example.test>"
	cfg.SMTPTLS = "tls"
	cfg.UmamiURL = "https://umami.env.test"
	cfg.UmamiWebsiteID = "env-site"
	cfg.AllowedEmails = []string{"A@Example.test"}
	cfg.AllowedDomains = []string{"@Example.test"}
	cfg.PasswordFallback = false

	if err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	snap = s.Snapshot()
	if snap.SiteName() != "Env Pop" || snap.Source(KeyAppName) != "env" {
		t.Fatalf("env app_name = %q/%q", snap.SiteName(), snap.Source(KeyAppName))
	}
	if snap.BaseURL() != "https://env.example.test" || snap.Source(KeyBaseURL) != "env" {
		t.Fatalf("env base url = %q/%q", snap.BaseURL(), snap.Source(KeyBaseURL))
	}
	smtp := snap.SMTP()
	if smtp.Host != "smtp.env.test" || smtp.Port != 2525 || smtp.Username != "env-user" || smtp.Password != "env-pass" || smtp.TLS != "tls" {
		t.Fatalf("env smtp = %+v", smtp)
	}
	if umami := snap.Umami(); umami.URL != "https://umami.env.test" || umami.WebsiteID != "env-site" {
		t.Fatalf("env umami = %+v", umami)
	}
	if got := snap.AllowedEmails(); len(got) != 1 || got[0] != "a@example.test" {
		t.Fatalf("env allowed emails = %v", got)
	}
	if got := snap.AllowedDomains(); len(got) != 1 || got[0] != "example.test" {
		t.Fatalf("env allowed domains = %v", got)
	}
	if snap.AllowPasswordFallback() {
		t.Fatal("env password fallback should be false")
	}

	// Database layer wins over env for runtime-safe keys.
	if err := s.Set(ctx, KeyAppName, "DB Pop", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, KeySMTPPort, "2526", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, KeyRegistrationOpen, "0", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, KeyAllowedEmails, "Db@Example.test, second@example.test", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	snap = s.Snapshot()
	if snap.SiteName() != "DB Pop" || snap.Source(KeyAppName) != "db" {
		t.Fatalf("db app_name = %q/%q", snap.SiteName(), snap.Source(KeyAppName))
	}
	if snap.SMTP().Port != 2526 || snap.Source(KeySMTPPort) != "db" {
		t.Fatalf("db smtp port = %d/%q", snap.SMTP().Port, snap.Source(KeySMTPPort))
	}
	if snap.RegistrationOpen() || snap.Source(KeyRegistrationOpen) != "db" {
		t.Fatalf("db registration = %v/%q", snap.RegistrationOpen(), snap.Source(KeyRegistrationOpen))
	}
	if got := snap.AllowedEmails(); len(got) != 2 || got[0] != "db@example.test" || got[1] != "second@example.test" {
		t.Fatalf("db allowed emails = %v", got)
	}

	// Deleting a row reverts to env.
	if err := s.Delete(ctx, KeyAppName, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, KeyRegistrationOpen, "admin-1"); err != nil {
		t.Fatal(err)
	}
	snap = s.Snapshot()
	if snap.SiteName() != "Env Pop" || snap.Source(KeyAppName) != "env" {
		t.Fatalf("after delete app_name = %q/%q", snap.SiteName(), snap.Source(KeyAppName))
	}
	if !snap.RegistrationOpen() || snap.Source(KeyRegistrationOpen) != "default" {
		t.Fatalf("after delete registration = %v/%q", snap.RegistrationOpen(), snap.Source(KeyRegistrationOpen))
	}
}

func TestSecretRoundTripMaskingAndBlankKeeps(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()
	cfg.SMTPPass = "env-secret"
	s, database := newTestService(t, cfg, "", nil)

	if err := s.Set(ctx, KeySMTPPassword, "hunter2!", true, "admin-1"); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if got := snap.SMTP().Password; got != "hunter2!" {
		t.Fatalf("password = %q", got)
	}
	if snap.Source(KeySMTPPassword) != "db" {
		t.Fatalf("source = %q, want db", snap.Source(KeySMTPPassword))
	}
	isSet, last4 := snap.SecretState(KeySMTPPassword)
	if !isSet || last4 != "er2!" {
		t.Fatalf("secret state = %v/%q", isSet, last4)
	}

	row, err := database.Client.RuntimeSetting.Query().
		Where(runtimesetting.KeyEQ(KeySMTPPassword)).
		Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if row.Value != "" || !row.IsSecret || !strings.HasPrefix(row.EncryptedValue, secretbox.Prefix) {
		t.Fatalf("stored secret row = value %q, secret %v, ciphertext %q", row.Value, row.IsSecret, row.EncryptedValue)
	}
	if strings.Contains(row.EncryptedValue, "hunter2!") {
		t.Fatal("ciphertext contains plaintext")
	}
	if row.UpdatedBy == nil || *row.UpdatedBy != "admin-1" {
		t.Fatalf("updated_by = %v", row.UpdatedBy)
	}

	// A blank submitted secret keeps the existing ciphertext untouched.
	if err := s.Set(ctx, KeySMTPPassword, "   ", true, "admin-1"); err != nil {
		t.Fatal(err)
	}
	again, err := database.Client.RuntimeSetting.Query().
		Where(runtimesetting.KeyEQ(KeySMTPPassword)).
		Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.EncryptedValue != row.EncryptedValue {
		t.Fatal("blank secret replaced the stored ciphertext")
	}

	// Clearing removes the row and reverts to the env secret.
	if err := s.Delete(ctx, KeySMTPPassword, "admin-1"); err != nil {
		t.Fatal(err)
	}
	snap = s.Snapshot()
	if got := snap.SMTP().Password; got != "env-secret" {
		t.Fatalf("after clear password = %q, want env-secret", got)
	}
	if snap.Source(KeySMTPPassword) != "env" {
		t.Fatalf("after clear source = %q, want env", snap.Source(KeySMTPPassword))
	}
	if isSet, _ := snap.SecretState(KeySMTPPassword); !isSet {
		t.Fatal("env secret should report isSet")
	}

	// Deleting a missing row is a no-op.
	if err := s.Delete(ctx, KeySMTPPassword, "admin-1"); err != nil {
		t.Fatal(err)
	}
}

func TestAuditContainsKeyNamesOnly(t *testing.T) {
	ctx := context.Background()
	s, database := newTestService(t, testConfig(), "", nil)

	if err := s.Set(ctx, KeySMTPPassword, "audit-plaintext", true, "admin-1"); err != nil {
		t.Fatal(err)
	}
	notes, err := database.Client.AdminNote.Query().
		Where(adminnote.AuthorIDEQ("admin-1")).
		All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 {
		t.Fatal("no audit note written")
	}
	for _, note := range notes {
		if strings.Contains(note.Body, "audit-plaintext") || strings.Contains(note.Body, secretbox.Prefix) {
			t.Fatalf("audit note leaked a secret: %q", note.Body)
		}
		if !strings.Contains(note.Body, KeySMTPPassword) {
			t.Fatalf("audit note %q does not name the key", note.Body)
		}
	}
}

func TestChangedSessionSecretFallsBackWithWarning(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()
	first, _ := newTestService(t, cfg, "first-session-secret", nil)
	if err := first.Set(ctx, KeySMTPPassword, "old-plaintext", true, "admin-1"); err != nil {
		t.Fatal(err)
	}
	row, err := first.client.RuntimeSetting.Query().
		Where(runtimesetting.KeyEQ(KeySMTPPassword)).
		Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := row.EncryptedValue

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	second, err := New(first.client, cfg, "second-session-secret", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Load(ctx); err != nil {
		t.Fatalf("load with changed secret should not fail: %v", err)
	}
	snap := second.Snapshot()
	if got := snap.SMTP().Password; got != "" {
		t.Fatalf("password after failed decrypt = %q, want empty fallback", got)
	}
	if snap.Source(KeySMTPPassword) != "default" {
		t.Fatalf("source = %q, want default", snap.Source(KeySMTPPassword))
	}
	if isSet, _ := snap.SecretState(KeySMTPPassword); isSet {
		t.Fatal("failed secret should not report isSet")
	}

	out := logs.String()
	if !strings.Contains(out, "could not be decrypted") || !strings.Contains(out, KeySMTPPassword) {
		t.Fatalf("missing warning in logs: %q", out)
	}
	if strings.Contains(out, ciphertext) || strings.Contains(out, "old-plaintext") {
		t.Fatalf("logs leaked secret material: %q", out)
	}
}

func TestInvalidValuesRejected(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t, testConfig(), "", nil)

	invalid := []struct {
		key   string
		value string
	}{
		{KeyAppName, "   "},
		{KeyBaseURL, "javascript:alert(1)"},
		{KeyBaseURL, "https://user:pass@example.test"},
		{KeyBaseURL, "https://bad_host.example.test"},
		{KeyBaseURL, "https://exa mple.test"},
		{KeySMTPPort, "0"},
		{KeySMTPPort, "65536"},
		{KeySMTPPort, "smtp"},
		{KeySMTPTLS, "plaintext"},
		{KeySMTPFrom, "Evil\r\nBcc: x@example.test"},
		{KeyUmamiURL, "ftp://umami.test"},
		{KeyUmamiURL, "https://user:pass@umami.test"},
		{KeyUmamiURL, "https://umami.test/script.js"},
		{KeyUmamiURL, "https://umami.test/?q=1"},
		{KeyUmamiURL, "//umami.test"},
		{KeyUmamiURL, "https://umami_host.test"},
		{KeyRegistrationOpen, "maybe"},
		{KeyAIEnabled, "2"},
		{KeyAllowedEmails, "not-an-email"},
		{KeyAllowedEmails, "a@example.test@"},
		{KeyAllowedDomains, "@"},
	}
	for _, tc := range invalid {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			err := s.Set(ctx, tc.key, tc.value, false, "admin-1")
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("Set(%s, %q) err = %v, want ErrInvalidValue", tc.key, tc.value, err)
			}
		})
	}

	if err := s.Set(ctx, KeyUmamiURL, "https://Umami.Example:8443/", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().Umami().URL; got != "https://umami.example:8443" {
		t.Fatalf("normalized umami = %q", got)
	}
	if err := s.Set(ctx, KeyBaseURL, "https://[::1]:8443/", false, "admin-1"); err != nil {
		t.Fatalf("IPv6 base URL rejected: %v", err)
	}
	if got := s.Snapshot().BaseURL(); got != "https://[::1]:8443" {
		t.Fatalf("IPv6 base URL = %q", got)
	}
	if err := s.Set(ctx, KeyUmamiURL, "https://[2001:db8::1]/", false, "admin-1"); err != nil {
		t.Fatalf("IPv6 Umami URL rejected: %v", err)
	}
	if err := s.Set(ctx, KeySMTPPort, " 25 ", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().SMTP().Port; got != 25 {
		t.Fatalf("normalized port = %d", got)
	}
	if err := s.Set(ctx, KeyRegistrationOpen, "yes", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().RegistrationOpen() {
		t.Fatal("yes should normalize to true")
	}
	if err := s.Set(ctx, KeySMTPTLS, "465", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().SMTP().TLS; got != "tls" {
		t.Fatalf("TLS 465 normalized to %q, want tls", got)
	}
	if err := s.Set(ctx, KeyAllowedDomains, "Example.COM, @other.test", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().AllowedDomains(); len(got) != 2 || got[0] != "example.com" || got[1] != "other.test" {
		t.Fatalf("domains = %v", got)
	}
}

func TestKeyValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t, testConfig(), "", nil)

	if err := s.Set(ctx, "SESSION_SECRET", "x", false, "admin-1"); !errors.Is(err, ErrEnvOnlyKey) {
		t.Fatalf("SESSION_SECRET err = %v, want ErrEnvOnlyKey", err)
	}
	if err := s.Set(ctx, "otel_exporter_otlp_endpoint", "x", false, "admin-1"); !errors.Is(err, ErrEnvOnlyKey) {
		t.Fatalf("otel key err = %v, want ErrEnvOnlyKey", err)
	}
	if err := s.Set(ctx, "nonsense", "x", false, "admin-1"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown key err = %v, want ErrUnknownKey", err)
	}
	if err := s.Set(ctx, KeyAppName, "x", true, "admin-1"); !errors.Is(err, ErrSecretKey) {
		t.Fatalf("secret on non-secret key err = %v, want ErrSecretKey", err)
	}
	if err := s.Delete(ctx, "PORT", "admin-1"); !errors.Is(err, ErrEnvOnlyKey) {
		t.Fatalf("delete PORT err = %v, want ErrEnvOnlyKey", err)
	}
}

func TestInvalidDatabaseRowIsIgnored(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()
	cfg.AppName = "Env Pop"
	s, database := newTestService(t, cfg, "", nil)
	if _, err := database.Client.RuntimeSetting.Create().
		SetKey(KeyRegistrationOpen).
		SetValue("not-a-bool").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().RegistrationOpen() || s.Snapshot().Source(KeyRegistrationOpen) != "default" {
		t.Fatalf("invalid row changed snapshot: %v/%q", s.Snapshot().RegistrationOpen(), s.Snapshot().Source(KeyRegistrationOpen))
	}
}

func TestGettersDoNotReadDatabase(t *testing.T) {
	ctx := context.Background()
	s, database := newTestService(t, testConfig(), "", nil)
	if err := s.Set(ctx, KeyAppName, "Cached Pop", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if got := snap.SiteName(); got != "Cached Pop" {
		t.Fatalf("site name after close = %q", got)
	}
	if snap.Source(KeyAppName) != "db" || snap.SMTP().Port != defaultSMTPPort {
		t.Fatal("getters depend on the database")
	}
}

func TestFailedWriteKeepsOldSnapshot(t *testing.T) {
	ctx := context.Background()
	s, database := newTestService(t, testConfig(), "", nil)
	if err := s.Set(ctx, KeyAppName, "Old Name", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, KeyAppName, "New Name", false, "admin-1"); err == nil {
		t.Fatal("Set succeeded against a closed database")
	}
	after := s.Snapshot()
	if after != before {
		t.Fatal("failed write swapped the snapshot")
	}
	if after.SiteName() != "Old Name" {
		t.Fatalf("site name = %q, want Old Name", after.SiteName())
	}
}

func TestSnapshotSwapIsAtomicForConcurrentReaders(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestService(t, testConfig(), "", nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					snap := s.Snapshot()
					if snap == nil || snap.SiteName() == "" || snap.SMTP().From == "" {
						t.Error("observed incomplete snapshot")
						return
					}
					_ = snap.AllowedEmails()
					_, _ = snap.SecretState(KeySMTPPassword)
				}
			}
		}()
	}

	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("Name %d", i)
		if err := s.Set(ctx, KeyAppName, name, false, "admin-1"); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			if err := s.Delete(ctx, KeyAppName, "admin-1"); err != nil {
				t.Fatal(err)
			}
		}
	}
	close(stop)
	wg.Wait()
}

func TestConcurrentWritesPublishLatestSnapshot(t *testing.T) {
	ctx := context.Background()
	s, database := newTestService(t, testConfig(), "", nil)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Set(ctx, KeyAppName, fmt.Sprintf("Name %d", i), false, "admin-1"); err != nil {
				t.Errorf("Set: %v", err)
			}
		}(i)
	}
	wg.Wait()

	row, err := database.Client.RuntimeSetting.Query().
		Where(runtimesetting.KeyEQ(KeyAppName)).
		Only(ctx)
	if err != nil {
		t.Fatalf("read final row: %v", err)
	}
	if got := s.Snapshot().SiteName(); got != row.Value {
		t.Fatalf("snapshot site name = %q, committed row = %q", got, row.Value)
	}
}

func TestConcurrentLoadCannotPublishStaleSnapshot(t *testing.T) {
	ctx := context.Background()
	s, database := newTestService(t, testConfig(), "", nil)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Load(ctx)
		}()
	}
	for i := 0; i < 20; i++ {
		if err := s.Set(ctx, KeyAppName, fmt.Sprintf("Committed %d", i), false, "admin-1"); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	wg.Wait()

	row, err := database.Client.RuntimeSetting.Query().
		Where(runtimesetting.KeyEQ(KeyAppName)).
		Only(ctx)
	if err != nil {
		t.Fatalf("read final row: %v", err)
	}
	if got := s.Snapshot().SiteName(); got != row.Value {
		t.Fatalf("snapshot site name = %q, committed row = %q", got, row.Value)
	}
}

func TestSnapshotMutationIsolation(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig()
	cfg.AllowedEmails = []string{"a@example.test"}
	s, _ := newTestService(t, cfg, "", nil)

	first := s.Snapshot().AllowedEmails()
	first[0] = "mutated@example.test"
	if got := s.Snapshot().AllowedEmails()[0]; got != "a@example.test" {
		t.Fatalf("snapshot list was mutated: %q", got)
	}

	if err := s.Set(ctx, KeyAllowedEmails, "b@example.test", false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().AllowedEmails()[0]; got != "b@example.test" {
		t.Fatalf("new snapshot = %q", got)
	}
}
