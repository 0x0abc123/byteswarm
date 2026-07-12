// Command byteswarm is the framework server binary (ADR-0001 modular monolith,
// ADR-0003 Go). It is the composition root: it configures structured logging,
// constructs the event bus, consumer registry, and inbound HTTP adapter, wires
// them together (constructor injection only, reference/design-principles.md),
// and serves until interrupted, shutting down gracefully.
//
// Configuration comes from a committable JSON file (BYTESWARM_CONFIG) with
// environment overrides (ADR-0006). When it declares plugins, main opens the
// SQLite state store, constructs the goja script-plugin host (internal/plugin,
// ADR-0008) with the configured exec allowlist, and registers the declared
// script plugins as consumers alongside compiled-in Go consumers.
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

	"github.com/0x0abc123/byteswarm/internal/auth"
	"github.com/0x0abc123/byteswarm/internal/bus"
	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
	"github.com/0x0abc123/byteswarm/internal/plugin"
	"github.com/0x0abc123/byteswarm/internal/server"
	"github.com/0x0abc123/byteswarm/internal/store"
)

// version is the server build version, overridable at link time via -ldflags
// (-X main.version=...); it defaults to a dev sentinel for local builds.
var version = "0.0.0-dev"

// noBusPublisher is the Publisher used when no event bus is configured
// (BYTESWARM_NATS_URL unset): POST /events fails closed rather than dropping
// events silently. Set BYTESWARM_NATS_URL to enable publishing.
type noBusPublisher struct{}

func (noBusPublisher) Publish(context.Context, event.Event) error {
	return errors.New("event bus not configured (set BYTESWARM_NATS_URL)")
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("starting byteswarm", slog.String("version", version))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig(os.Getenv("BYTESWARM_CONFIG"))
	if err != nil {
		logger.Error("loading configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Wire the event bus and consumer registry if a broker is configured.
	// Without one, the server still serves health endpoints; POST /events
	// fails closed via noBusPublisher.
	var pub event.Publisher = noBusPublisher{}
	if cfg.NATSURL != "" {
		b, err := bus.New(bus.Config{URL: cfg.NATSURL, Name: "byteswarm-server"}, logger)
		if err != nil {
			logger.Error("connecting to event bus", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer func() { _ = b.Close() }()
		pub = b

		reg := consumer.NewRegistry(logger)
		reg.Register(newExampleConsumer(logger), exampleEventType)

		// Runtime script plugins (ADR-0008), when declared in config. Register
		// them before Run so they are subscribed before the first delivery.
		if len(cfg.Plugins) > 0 {
			repo, err := store.NewSQLite(cfg.Store.Path)
			if err != nil {
				logger.Error("opening plugin state store", slog.String("error", err.Error()))
				os.Exit(1)
			}
			defer func() { _ = repo.Close() }()

			host := plugin.NewHost(repo, b, plugin.ExecAllowlist(cfg.ExecAllow), cfg.PluginsDir, logger)
			if d, set, _ := cfg.pluginTimeout(); set {
				// Validated in loadConfig; override the host default before loading.
				host.Timeout = d
			}
			n, err := registerPlugins(reg, host, plugin.Config{Plugins: cfg.Plugins})
			if err != nil {
				logger.Error("loading script plugins", slog.String("error", err.Error()))
				os.Exit(1)
			}
			logger.Info("script plugins loaded", slog.Int("count", n))
		} else {
			logger.Info("no script plugins configured")
		}

		go func() {
			if err := reg.Run(ctx, b, cfg.WorkflowID); err != nil {
				logger.Error("consumer registry stopped", slog.String("error", err.Error()))
				stop()
			}
		}()
		scope := cfg.WorkflowID
		if scope == "" {
			scope = "any"
		}
		logger.Info("event bus connected; consumer registry running",
			slog.String("nats_url", cfg.NATSURL), slog.String("workflow_scope", scope))
	} else {
		logger.Warn("no NATS URL configured: running without event bus; POST /events will fail until configured")
	}

	// Shared-secret authenticator for the webhook ingress (ADR-0002). An empty
	// secret (BYTESWARM_WEBHOOK_SECRET unset) denies all callers — /webhook is
	// effectively closed until a secret is configured (fail closed).
	webhookAuth := auth.NewSharedSecret(cfg.WebhookSecret)
	if cfg.WebhookSecret == "" {
		logger.Warn("BYTESWARM_WEBHOOK_SECRET not set: POST /webhook will reject all requests until configured")
	}

	handlers := server.New(logger, pub, webhookAuth)

	// Split ingress by transport (ADR-0011). Both bound with a read-header
	// timeout to defend against slow-client attacks
	// (reference/security-fundamentals.md: request timeouts on every server).
	//
	// /events: operator-local, over a Unix domain socket whose file permissions
	// are the access control (defence in depth, no app-layer auth).
	eventsLn, err := listenUnix(cfg.Socket)
	if err != nil {
		logger.Error("binding events socket", slog.String("error", err.Error()))
		os.Exit(1)
	}
	eventsSrv := &http.Server{
		Handler:           handlers.Events,
		ReadHeaderTimeout: 10 * time.Second,
	}
	// /webhook + health: over TCP for authenticated, cross-host, untrusted callers.
	controlSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handlers.Control,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("byteswarm events ingress listening", slog.String("socket", cfg.Socket.Path))
		if err := eventsSrv.Serve(eventsLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("events ingress error", slog.String("error", err.Error()))
			stop()
		}
	}()
	go func() {
		logger.Info("byteswarm control ingress listening", slog.String("addr", cfg.HTTPAddr))
		if err := controlSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("control ingress error", slog.String("error", err.Error()))
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Shut down both ingresses; the Unix listener unlinks its socket on close.
	var shutdownErr error
	if err := eventsSrv.Shutdown(shutdownCtx); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	if err := controlSrv.Shutdown(shutdownCtx); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	if shutdownErr != nil {
		logger.Error("graceful shutdown failed", slog.String("error", shutdownErr.Error()))
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}
