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
	"sodapop/internal/demo"
	"sodapop/internal/email"
	"sodapop/internal/geocode"
	"sodapop/internal/integrations"
	"sodapop/internal/media"
	"sodapop/internal/mix"
	"sodapop/internal/pairings"
	"sodapop/internal/ratelimit"
	"sodapop/internal/savedsearch"
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
	level, levelErr := config.ParseLogLevel(cfg.LogLevel)
	var baseHandler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if cfg.Env == "development" {
		baseHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		baseHandler = slog.NewJSONHandler(os.Stdout, opts)
	}
	log := slog.New(baseHandler)
	slog.SetDefault(log)
	if levelErr != nil {
		log.Warn("unrecognised LOG_LEVEL; using info", "value", cfg.LogLevel)
	}
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
		telCfg.MetricsEndpoint = signalEndpoint(telCfg.Endpoint, "metrics")
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") == "" {
		telCfg.LogsEndpoint = signalEndpoint(telCfg.Endpoint, "logs")
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
	metricsSetup, err := telemetry.SetupMetrics(context.Background(), telCfg)
	if err != nil {
		log.Warn("metrics disabled", "err", err)
		metricsSetup = &telemetry.MetricsSetup{Shutdown: func(context.Context) error { return nil }}
	}
	shutdownMetrics := metricsSetup.Shutdown

	// Keep JSON/console output and fan the same records out over OTLP. The
	// handler is rebuilt only when log export is actually configured.
	shutdownLogs := func(context.Context) error { return nil }
	if otelLogs, shutdown, err := telemetry.SetupLogs(context.Background(), telCfg); err != nil {
		log.Warn("log export disabled", "err", err)
	} else if otelLogs != nil {
		log = slog.New(telemetry.Fanout(baseHandler, otelLogs))
		slog.SetDefault(log)
		shutdownLogs = shutdown
	}

	if telCfg.Enabled {
		log.Info("telemetry enabled", "endpoint", telCfg.Endpoint, "sampler", telCfg.Sampler)
	}
	if telCfg.LogsEndpoint != "" && !telCfg.Disabled {
		log.Info("log export enabled", "endpoint", telCfg.LogsEndpoint)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownLogs(ctx); err != nil {
			log.Warn("logs shutdown", "err", err)
		}
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
	if err := demo.Ensure(context.Background(), database.Client, cfg, checkinSvc); err != nil {
		log.Error("demo account could not be provisioned", "err", err)
	} else if cfg.DemoUserEnabled {
		log.Info("demo account ready", "handle", cfg.DemoUserHandle)
	}
	savesSvc := saves.New(database, checkinSvc)
	savedSearchSvc := savedsearch.New(database, socialSvc)
	// A drink that becomes visible is a catalog drop for matching saved
	// searches. The catalog only needs the narrow Evaluate hook.
	catalogSvc.Drops = savedSearchSvc
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
	// The recap filters awarded badges by year; it reads them through the same
	// list the badge endpoint serves.
	checkinSvc.Badges = badgeSvc
	tasteSvc := taste.New(database)
	mixSvc := mix.New(database)
	pairingSvc := pairings.New(cfg.SandwitchesBaseURL)
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
		Pairings:   pairingSvc,
		Nearby:     geocode.New(cfg.NearbyGeocoderEnabled, cfg.NearbyGeocoderURL, cfg.NearbyGeocoderUserAgent, cfg.NearbyGeocoderTimeout),
	}
	apiSrv := &api.Server{
		Cfg: cfg, Auth: authSvc, Catalog: catalogSvc, Checkins: checkinSvc,
		Saves:    savesSvc,
		Comments: commentSvc,
		Social:   socialSvc, Venues: venueSvc, Mailer: mailer, Log: log,
		Limiter: limiter, DB: database, Media: mediaSvc, AI: aiSvc,
		Integrations: integrationSvc, Badges: badgeSvc,
		Taste: tasteSvc, Mix: mixSvc, Account: accountSvc,
		Nearby: geocode.New(cfg.NearbyGeocoderEnabled, cfg.NearbyGeocoderURL, cfg.NearbyGeocoderUserAgent, cfg.NearbyGeocoderTimeout),
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
	if metricsSetup.Prometheus != nil {
		mux.Handle("GET /metrics", metricsSetup.Prometheus)
	}

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

// signalEndpoint keeps a single editable OTLP endpoint usable for every
// signal. A signal-specific path is translated; a base URL is passed through
// and the exporter appends the standard suffix.
func signalEndpoint(endpoint, signal string) string {
	for _, current := range []string{"traces", "metrics", "logs"} {
		suffix := "/v1/" + current
		if strings.HasSuffix(endpoint, suffix) {
			return strings.TrimSuffix(endpoint, suffix) + "/v1/" + signal
		}
	}
	return endpoint
}
