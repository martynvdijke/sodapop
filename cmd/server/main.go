package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"sodapop/internal/account"
	"sodapop/internal/ai"
	"sodapop/internal/api"
	"sodapop/internal/auth"
	"sodapop/internal/backup"
	"sodapop/internal/badges"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/email"
	"sodapop/internal/integrations"
	"sodapop/internal/media"
	"sodapop/internal/mix"
	"sodapop/internal/ratelimit"
	"sodapop/internal/saves"
	"sodapop/internal/settings"
	"sodapop/internal/setup"
	"sodapop/internal/sharelinks"
	"sodapop/internal/social"
	"sodapop/internal/taste"
	"sodapop/internal/telemetry"
	"sodapop/internal/venues"
	"sodapop/internal/web"
)

// Version is injected at build time with -ldflags "-X main.Version=...".
var Version = "0.0.0-dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if Version != "0.0.0-dev" {
		cfg.Version = Version
	}
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.Env == "development" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	log := slog.New(handler)
	slog.SetDefault(log)

	database, err := db.Init(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	backupSvc := backup.New(database, filepath.Join(cfg.DataDir, "backups"), 14)

	settingsSvc, err := settings.New(database.Client, cfg, string(cfg.SessionKey), log)
	if err != nil {
		return err
	}
	if err := settingsSvc.Load(context.Background()); err != nil {
		log.Error("runtime settings could not be loaded", "err", err)
		return err
	}

	snap := settingsSvc.Snapshot()
	telCfg := telemetry.FromEnv(cfg.Version)
	telCfg.Endpoint = snap.OTELEndpoint()
	if os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" {
		telCfg.MetricsEndpoint = metricsEndpoint(telCfg.Endpoint)
	}
	if name := snap.OTELServiceName(); name != "" {
		telCfg.ServiceName = name
	}
	telCfg.Sampler = snap.OTELSampler()
	telCfg.Disabled = snap.OTELDisabled()
	telCfg.Enabled = !telCfg.Disabled && telCfg.Endpoint != ""

	shutdownTelemetry, err := telemetry.Setup(context.Background(), telCfg)
	if err != nil {
		log.Warn("telemetry disabled", "err", err)
		shutdownTelemetry = func(context.Context) error { return nil }
	}
	shutdownMetrics, err := telemetry.SetupMetrics(context.Background(), telCfg)
	if err != nil {
		log.Warn("metrics disabled", "err", err)
		shutdownMetrics = func(context.Context) error { return nil }
	}
	if telCfg.Enabled {
		log.Info("telemetry enabled", "endpoint", telCfg.Endpoint, "sampler", telCfg.Sampler)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownMetrics(ctx); err != nil {
			log.Warn("metrics shutdown", "err", err)
		}
		if err := shutdownTelemetry(ctx); err != nil {
			log.Warn("telemetry shutdown", "err", err)
		}
	}()

	authSvc := auth.New(cfg, database, settingsSvc)
	if err := authSvc.InitOIDC(context.Background()); err != nil {
		log.Warn("oidc disabled", "err", err)
	}

	mailer := email.New(cfg, database, log, settingsSvc)
	catalogSvc := catalog.New(cfg, database)
	socialSvc := social.New(database)
	checkinSvc := checkins.New(database, socialSvc)
	savesSvc := saves.New(database, checkinSvc)
	commentSvc := comments.New(database, checkinSvc)
	venueSvc := venues.New(database)
	mediaSvc := media.New(database, cfg)
	aiSvc := ai.New(database, cfg, cfg.Version, log, settingsSvc)
	integrationSvc := integrations.New(database, cfg, log, settingsSvc)
	// Mirror push-worthy in-app notifications to Web Push/outbound integrations.
	// Badge and system notifications stay in-app only (badge push is dispatched
	// elsewhere).
	socialSvc.OnNotify = func(ctx context.Context, userID, kind, actorID, subjectID, text string) {
		var eventKind, target string
		switch kind {
		case "friend_request", "friend_accept":
			eventKind, target = integrations.EventFriend, "/friends"
		case "toast":
			eventKind, target = integrations.EventToast, "/"
			if subjectID != "" {
				target = "/checkins/" + subjectID
			}
		case "comment", "mention":
			eventKind, target = integrations.EventCheckin, "/"
			if subjectID != "" {
				target = "/checkins/" + subjectID + "#comments"
			}
		default:
			return
		}
		if err := integrationSvc.Dispatch(ctx, integrations.Event{
			UserID: userID,
			Kind:   eventKind,
			Text:   text,
			URL:    target,
		}); err != nil {
			log.WarnContext(ctx, "notification push dispatch failed", "kind", kind, "err", err)
		}
	}
	accountSvc := account.New(database.Client, integrationSvc)
	telemetry.SetOutboxDepthFunc(func(ctx context.Context) (int64, error) {
		n, err := integrationSvc.OutboxDepth(ctx)
		return int64(n), err
	})
	badgeSvc := badges.New(database)
	tasteSvc := taste.New(database)
	mixSvc := mix.New(database)
	limiter := ratelimit.New()
	setupSvc := setup.New(database.Client, authSvc, limiter, log, os.Getenv("SETUP_TOKEN"))

	renderer, err := web.NewRenderer()
	if err != nil {
		return err
	}

	webSrv := &web.Server{
		Cfg: cfg, Auth: authSvc, Catalog: catalogSvc, Checkins: checkinSvc,
		Saves:  savesSvc,
		Social: socialSvc, Venues: venueSvc, Mailer: mailer, Log: log,
		Badges: badgeSvc, Taste: tasteSvc, Mix: mixSvc,
		DB: database, Media: mediaSvc, AI: aiSvc, Integrations: integrationSvc,
		Account: accountSvc,
		Backup:  backupSvc,
		Limiter: limiter, Render: renderer, Setup: setupSvc, Settings: settingsSvc,
		ShareLinks: sharelinks.New(database.SQL),
	}
	apiSrv := &api.Server{
		Cfg: cfg, Auth: authSvc, Catalog: catalogSvc, Checkins: checkinSvc,
		Saves:    savesSvc,
		Comments: commentSvc,
		Social:   socialSvc, Venues: venueSvc, Mailer: mailer, Log: log,
		Limiter: limiter, DB: database, Media: mediaSvc, AI: aiSvc,
		Integrations: integrationSvc, Badges: badgeSvc,
		Taste: tasteSvc, Mix: mixSvc, Account: accountSvc,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := database.SQL.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	apiSrv.Register(mux)
	webSrv.Register(mux)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go mailer.Run(ctx)
	go integrationSvc.RunOutbox(ctx)
	go integrationSvc.RunDigestScheduler(ctx)
	go backupSvc.Run(ctx)
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				limiter.Prune(30 * time.Minute)
				// Draft sessions expire after their TTL; keep the sweep
				// bounded so one slow delete cannot stall maintenance.
				sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				if n, err := aiSvc.ExpireDrafts(sweepCtx); err != nil {
					log.Warn("expire ai drafts failed", "err", err)
				} else if n > 0 {
					log.Info("expired ai drafts", "count", n)
				}
				cancel()
			}
		}
	}()

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           telemetry.Handler(webSrv.Middleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("sodapop listening", "addr", httpServer.Addr, "version", cfg.Version)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// metricsEndpoint keeps a single editable OTLP endpoint usable for both
// signals. A trace-specific path is translated to the metrics path; a base
// URL is passed through and the exporter appends the standard suffix.
func metricsEndpoint(endpoint string) string {
	if strings.HasSuffix(endpoint, "/v1/traces") {
		return strings.TrimSuffix(endpoint, "/v1/traces") + "/v1/metrics"
	}
	return endpoint
}
