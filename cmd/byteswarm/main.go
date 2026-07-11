// Command byteswarm is the framework server binary (ADR-0001 modular monolith,
// ADR-0003 Go). It is the composition root: it configures structured logging,
// constructs the event bus, consumer registry, and inbound HTTP adapter, wires
// them together (constructor injection only, reference/design-principles.md),
// and serves until interrupted, shutting down gracefully.
//
// When BYTESWARM_PLUGINS_CONFIG is set it also opens the SQLite state store,
// constructs the goja script-plugin host (internal/plugin, ADR-0008), and
// registers the declared script plugins as consumers alongside compiled-in
// Go consumers.
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

	"github.com/0x0abc123/byteswarm/internal/bus"
	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
	"github.com/0x0abc123/byteswarm/internal/plugin"
	"github.com/0x0abc123/byteswarm/internal/server"
	"github.com/0x0abc123/byteswarm/internal/store"
)

// noBusPublisher is the Publisher used when no event bus is configured
// (BYTESWARM_NATS_URL unset): POST /events fails closed rather than dropping
// events silently. Set BYTESWARM_NATS_URL to enable publishing.
type noBusPublisher struct{}

func (noBusPublisher) Publish(context.Context, event.Event) error {
	return errors.New("event bus not configured (set BYTESWARM_NATS_URL)")
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("BYTESWARM_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Wire the event bus and consumer registry if a broker is configured.
	// Without one, the server still serves health endpoints; POST /events
	// fails closed via noBusPublisher.
	var pub event.Publisher = noBusPublisher{}
	if natsURL := os.Getenv("BYTESWARM_NATS_URL"); natsURL != "" {
		b, err := bus.New(bus.Config{URL: natsURL, Name: "byteswarm-server"}, logger)
		if err != nil {
			logger.Error("connecting to event bus", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer func() { _ = b.Close() }()
		pub = b

		reg := consumer.NewRegistry(logger)
		reg.Register(newExampleConsumer(logger), exampleEventType)

		// Runtime script plugins (ADR-0008), when configured. Register them
		// before Run so they are subscribed before the first delivery.
		if cfgPath := os.Getenv("BYTESWARM_PLUGINS_CONFIG"); cfgPath != "" {
			repo, err := store.NewSQLite(storePath())
			if err != nil {
				logger.Error("opening plugin state store", slog.String("error", err.Error()))
				os.Exit(1)
			}
			defer func() { _ = repo.Close() }()

			// The exec allowlist stays empty here (deny all exec); it is
			// populated from the config file in a later feature (F2.4).
			host := plugin.NewHost(repo, b, plugin.ExecAllowlist{}, pluginsDir(), logger)
			consumers, err := buildPluginConsumers(host, cfgPath)
			if err != nil {
				logger.Error("loading script plugins", slog.String("error", err.Error()))
				os.Exit(1)
			}
			for _, sc := range consumers {
				reg.RegisterSubscriber(sc)
			}
			logger.Info("script plugins loaded", slog.Int("count", len(consumers)))
		} else {
			logger.Info("script plugins disabled (BYTESWARM_PLUGINS_CONFIG not set)")
		}

		go func() {
			if err := reg.Run(ctx, b); err != nil {
				logger.Error("consumer registry stopped", slog.String("error", err.Error()))
				stop()
			}
		}()
		logger.Info("event bus connected; consumer registry running",
			slog.String("nats_url", natsURL))
	} else {
		logger.Warn("BYTESWARM_NATS_URL not set: running without event bus; POST /events will fail until configured")
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: server.New(logger, pub),
		// Bound the request read to defend against slow-client attacks
		// (reference/security-fundamentals.md: request timeouts on every server).
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("byteswarm server listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.String("error", err.Error()))
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}
