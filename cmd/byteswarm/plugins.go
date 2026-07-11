package main

import (
	"fmt"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/plugin"
)

// registerPlugins loads (compiles) every declared plugin via the host and
// registers each as a ScriptConsumer on the registry. It fails closed: a
// plugin that will not compile aborts the whole set so the server can refuse
// to start rather than run with a partially-loaded plugin set (ADR-0008).
// Returns the number of plugins registered.
func registerPlugins(reg *consumer.Registry, host *plugin.Host, pcfg plugin.Config) (int, error) {
	consumers, err := host.LoadAll(pcfg)
	if err != nil {
		return 0, fmt.Errorf("loading script plugins: %w", err)
	}
	for _, sc := range consumers {
		reg.RegisterSubscriber(sc)
	}
	return len(consumers), nil
}
