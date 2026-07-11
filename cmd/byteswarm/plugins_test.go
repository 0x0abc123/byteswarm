package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/plugin"
	"github.com/0x0abc123/byteswarm/internal/store"
)

func pluginTestHost(t *testing.T) *plugin.Host {
	t.Helper()
	repo, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// noBusPublisher is only invoked if a script calls host.publish, which
	// loading (parse+compile only) never does.
	return plugin.NewHost(repo, noBusPublisher{}, plugin.ExecAllowlist{}, t.TempDir(), log)
}

func TestRegisterPluginsLoadsAndRegisters(t *testing.T) {
	reg := consumer.NewRegistry(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	host := pluginTestHost(t)

	pcfg := plugin.Config{Plugins: []plugin.PluginConfig{
		{Name: "greet", Events: []string{"order.created"}, Script: "1"},
	}}
	n, err := registerPlugins(reg, host, pcfg)
	if err != nil {
		t.Fatalf("registerPlugins: %v", err)
	}
	if n != 1 {
		t.Fatalf("registered %d plugins, want 1", n)
	}
}

func TestRegisterPluginsFailsClosedOnBadScript(t *testing.T) {
	reg := consumer.NewRegistry(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	host := pluginTestHost(t)

	pcfg := plugin.Config{Plugins: []plugin.PluginConfig{
		{Name: "broken", Events: []string{"x"}, Script: "function ("},
	}}
	if _, err := registerPlugins(reg, host, pcfg); err == nil {
		t.Fatal("registerPlugins with an uncompilable script should return an error (fail closed)")
	}
}
