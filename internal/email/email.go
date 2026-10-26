package email

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/emailoutbox"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/settings"
)

// Mailer owns the durable email outbox.
type Mailer struct {
	Cfg    *config.Config
	Client *ent.Client
	Log    *slog.Logger
	// Settings is optional. When present, SMTP and base URL values are read
	// from its snapshot for every delivery so admin changes apply immediately.
	Settings *settings.Service
}

func New(cfg *config.Config, database *db.DB, log *slog.Logger, runtime ...*settings.Service) *Mailer {
	m := &Mailer{Cfg: cfg, Client: database.Client, Log: log}
	if len(runtime) > 0 {
		m.Settings = runtime[0]
	}
	return m
}

// smtp returns the effective SMTP configuration. It reads the settings
// snapshot, falling back to the static config when no service is wired.
func (m *Mailer) smtp() settings.SMTPSettings {
	if m.Settings != nil {
		if snap := m.Settings.Snapshot(); snap != nil {
			return snap.SMTP()
		}
	}
	return settings.SMTPSettings{
		Host:     m.Cfg.SMTPHost,
		Port:     m.Cfg.SMTPPort,
		Username: m.Cfg.SMTPUser,
		Password: m.Cfg.SMTPPass,
		From:     m.Cfg.SMTPFrom,
		TLS:      m.Cfg.SMTPTLS,
	}
}

// baseURL returns the effective public base URL without a trailing slash.
func (m *Mailer) baseURL() string {
	if m.Settings != nil {
		if snap := m.Settings.Snapshot(); snap != nil {
			return snap.BaseURL()
		}
	}
	return strings.TrimRight(m.Cfg.BaseURL, "/")
}

// SendTest delivers one message immediately to the given address using the
// current runtime SMTP settings. It bypasses the outbox and is intended for
// the admin settings panel. With no SMTP host the development sink is used.
func (m *Mailer) SendTest(ctx context.Context, to string) error {
	body := "This is a test message from your Sodapop instance. If you can read it, outgoing mail is configured correctly.\n"
	return m.deliver(to, payload{Subject: "Sodapop test email", Body: body})
}

type payload struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Link    string `json:"link,omitempty"`
}

// Verification queues a verification email for a local account.
func (m *Mailer) Verification(ctx context.Context, to, handle, token string) error {
	link := m.baseURL() + "/verify?token=" + token
	body := fmt.Sprintf("Hi %s,\n\nWelcome to Sodapop. Confirm your email address to start logging sodas:\n\n%s\n\nThis link expires in 24 hours. If you did not create an account, ignore this message.\n", handle, link)
	return m.enqueue(ctx, to, "verification", payload{Subject: "Confirm your Sodapop email", Body: body, Link: link})
}

// PasswordReset queues a password reset email.
func (m *Mailer) PasswordReset(ctx context.Context, to, handle, token string) error {
	link := m.baseURL() + "/reset?token=" + token
	body := fmt.Sprintf("Hi %s,\n\nReset your Sodapop password here:\n\n%s\n\nThis link expires in one hour. If you did not request a reset, ignore this message.\n", handle, link)
	return m.enqueue(ctx, to, "password_reset", payload{Subject: "Reset your Sodapop password", Body: body, Link: link})
}

// FriendRequest queues a notification email (opt-out is checked by the caller).
func (m *Mailer) FriendRequest(ctx context.Context, to, handle, from string) error {
	body := fmt.Sprintf("Hi %s,\n\n%s sent you a friend request on Sodapop. Open %s/friends to respond.\n", handle, from, m.baseURL())
	return m.enqueue(ctx, to, "friend_request", payload{Subject: from + " wants to be friends on Sodapop", Body: body})
}

func (m *Mailer) enqueue(ctx context.Context, to, template string, p payload) error {
	raw, _ := json.Marshal(p)
	_, err := m.Client.EmailOutbox.Create().
		SetToEmail(to).
		SetTemplate(template).
		SetPayloadJSON(string(raw)).
		SetNextAttemptAt(db.Now()).
		Save(ctx)
	return err
}

// Run drains due messages until ctx is cancelled.
func (m *Mailer) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.drain(ctx); err != nil {
				m.Log.Warn("email outbox drain failed", "err", err)
			}
		}
	}
}

func (m *Mailer) drain(ctx context.Context) error {
	now := db.Now()
	rows, err := m.Client.EmailOutbox.Query().
		Where(
			emailoutbox.SentAtIsNil(),
			emailoutbox.NextAttemptAtLTE(now),
			emailoutbox.AttemptsLT(5),
		).
		Order(ent.Asc(emailoutbox.FieldNextAttemptAt)).
		Limit(20).
		All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var p payload
		if err := json.Unmarshal([]byte(row.PayloadJSON), &p); err != nil {
			_ = m.fail(ctx, row, err)
			continue
		}
		if err := m.deliver(row.ToEmail, p); err != nil {
			_ = m.fail(ctx, row, err)
			continue
		}
		_, _ = m.Client.EmailOutbox.UpdateOne(row).SetSentAt(db.Now()).Save(ctx)
	}
	return nil
}

func (m *Mailer) fail(ctx context.Context, row *ent.EmailOutbox, cause error) error {
	backoff := time.Duration(1<<uint(min(row.Attempts, 6))) * time.Minute
	_, err := m.Client.EmailOutbox.UpdateOne(row).
		SetAttempts(row.Attempts + 1).
		SetLastError(cause.Error()).
		SetNextAttemptAt(time.Now().UTC().Add(backoff).Format(time.RFC3339)).
		Save(ctx)
	return err
}

func (m *Mailer) deliver(to string, p payload) error {
	smtpCfg := m.smtp()
	if smtpCfg.Host == "" {
		// Development sink: one .eml file per message so verification links are
		// recoverable without a mail server.
		if err := os.MkdirAll(m.Cfg.EmailDevDir, 0o750); err != nil {
			return err
		}
		name := fmt.Sprintf("%d-%s.eml", time.Now().UnixNano(), strings.NewReplacer("@", "_at_", "/", "_").Replace(to))
		raw := "To: " + to + "\r\nFrom: " + smtpCfg.From + "\r\nSubject: " + p.Subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + p.Body
		return os.WriteFile(filepath.Join(m.Cfg.EmailDevDir, name), []byte(raw), 0o640)
	}
	addr := net.JoinHostPort(smtpCfg.Host, fmt.Sprintf("%d", smtpCfg.Port))
	msg := []byte("To: " + to + "\r\nFrom: " + smtpCfg.From + "\r\nSubject: " + p.Subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + p.Body)
	var auth smtp.Auth
	if smtpCfg.Username != "" {
		auth = smtp.PlainAuth("", smtpCfg.Username, smtpCfg.Password, smtpCfg.Host)
	}
	switch strings.ToLower(smtpCfg.TLS) {
	case "tls", "465":
		return smtpSendTLS(addr, smtpCfg.Host, auth, smtpCfg.From, to, msg)
	default:
		return smtp.SendMail(addr, auth, smtpCfg.From, []string{to}, msg)
	}
}

func smtpSendTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
