package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sodapop/internal/ai"
	"sodapop/internal/api"
	"sodapop/internal/auth"
	"sodapop/internal/badges"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/email"
	"sodapop/internal/integrations"
	"sodapop/internal/media"
	"sodapop/internal/mix"
	"sodapop/internal/ratelimit"
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

	telCfg := telemetry.FromEnv(cfg.Version)
	shutdownTelemetry, err := telemetry.Setup(context.Background(), telCfg)
	if err != nil {
		log.Warn("telemetry disabled", "err", err)
		shutdownTelemetry = func(context.Context) error { return nil }
	}
	if telCfg.Enabled {
		log.Info("telemetry enabled", "endpoint", telCfg.Endpoint, "sampler", telCfg.Sampler)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			log.Warn("telemetry shutdown", "err", err)
		}
	}()

	database, err := db.Init(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	authSvc := auth.New(cfg, database)
	if err := authSvc.InitOIDC(context.Background()); err != nil {
		log.Warn("oidc disabled", "err", err)
	}

	mailer := email.New(cfg, database, log)
	catalogSvc := catalog.New(cfg, database)
	socialSvc := social.New(database)
	checkinSvc := checkins.New(database, socialSvc)
	venueSvc := venues.New(database)
	mediaSvc := media.New(database, cfg)
	aiSvc := ai.New(database, cfg, cfg.Version, log)
	integrationSvc := integrations.New(database, cfg, log)
	badgeSvc := badges.New(database)
	tasteSvc := taste.New(database)
	mixSvc := mix.New(database)
	limiter := ratelimit.New()

	renderer, err := web.NewRenderer()
	if err != nil {
		return err
	}

	webSrv := &web.Server{
		Cfg: cfg, Auth: authSvc, Catalog: catalogSvc, Checkins: checkinSvc,
		Social: socialSvc, Venues: venueSvc, Mailer: mailer, Log: log,
		Badges: badgeSvc, Taste: tasteSvc, Mix: mixSvc,
		DB: database, Media: mediaSvc, AI: aiSvc, Integrations: integrationSvc,
		Limiter: limiter, Render: renderer,
	}
	apiSrv := &api.Server{
		Cfg: cfg, Auth: authSvc, Catalog: catalogSvc, Checkins: checkinSvc,
		Social: socialSvc, Venues: venueSvc, Mailer: mailer, Log: log,
		Limiter: limiter, DB: database, Media: mediaSvc, AI: aiSvc,
		Integrations: integrationSvc, Badges: badgeSvc,
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
	go integrationSvc.RunDigestScheduler(ctx)
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				limiter.Prune(30 * time.Minute)
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
