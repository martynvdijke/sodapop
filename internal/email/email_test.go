package email

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/settings"
)

// fakeSMTPServer accepts one message and reports the session commands.
func fakeSMTPServer(t *testing.T) (host string, port int, lines <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().(*net.TCPAddr)
	ch := make(chan string, 32)
	go func() {
		defer close(ch)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.WriteString(conn, "220 fake ESMTP\r\n")
		scanner := bufio.NewScanner(conn)
		inData := false
		for scanner.Scan() {
			line := scanner.Text()
			ch <- line
			if inData {
				if line == "." {
					inData = false
					_, _ = io.WriteString(conn, "250 queued\r\n")
				}
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				_, _ = io.WriteString(conn, "250 fake\r\n")
			case strings.HasPrefix(line, "DATA"):
				inData = true
				_, _ = io.WriteString(conn, "354 send it\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = io.WriteString(conn, "221 bye\r\n")
				return
			default:
				_, _ = io.WriteString(conn, "250 ok\r\n")
			}
		}
	}()
	return addr.IP.String(), addr.Port, ch
}

func TestSendTestUsesRuntimeSettings(t *testing.T) {
	host, port, lines := fakeSMTPServer(t)
	database, err := db.Init(filepath.Join(t.TempDir(), "email.db"))
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := &config.Config{
		SessionKey:  []byte("0123456789abcdef0123456789abcdef"),
		EmailDevDir: t.TempDir(),
		SMTPPort:    587,
		SMTPFrom:    "default@example.test",
		SMTPTLS:     "starttls",
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := settings.New(database.Client, cfg, "", log)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := svc.Load(context.Background()); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	for key, value := range map[string]string{
		settings.KeySMTPHost: fmt.Sprintf("%s", host),
		settings.KeySMTPPort: fmt.Sprintf("%d", port),
		settings.KeySMTPFrom: "runtime@example.test",
		settings.KeySMTPTLS:  "none",
	} {
		if err := svc.Set(context.Background(), key, value, false, "admin"); err != nil {
			t.Fatalf("settings.Set(%s): %v", key, err)
		}
	}

	mailer := New(cfg, database, log, svc)
	if err := mailer.SendTest(context.Background(), "admin@example.com"); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	var got []string
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				if !containsLine(got, "RCPT TO:<admin@example.com>") {
					t.Fatalf("SMTP session did not target the admin: %v", got)
				}
				return
			}
			got = append(got, line)
		case <-timeout:
			t.Fatalf("timed out waiting for SMTP session: %v", got)
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
