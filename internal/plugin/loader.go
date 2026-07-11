package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Config is the parsed `plugins` section of the byteswarm JSON config file
// (ADR-0006/0008). Each PluginConfig declares one script consumer.
type Config struct {
	Plugins []PluginConfig `json:"plugins"`
}

// PluginConfig declares one runtime script plugin: its host-controlled name
// (also the store namespace and sandbox directory name), the event types it
// subscribes to, and its source — either a Path resolved within the
// host-configured plugins directory (primary) or an inline Script string for
// trivial handlers. Exactly one source form must be set.
type PluginConfig struct {
	Name   string   `json:"name"`
	Events []string `json:"events"`
	Path   string   `json:"path,omitempty"`
	Script string   `json:"script,omitempty"`
}

// Parse reads and validates plugin declarations from JSON config bytes. It
// fails closed (ADR-0008): unknown fields, malformed JSON, or an invalid
// declaration return an error and load nothing — a plugin that does not parse
// does not start. Script compilation (which needs the goja runtime) happens
// after parse, at load, in the host.
func Parse(data []byte) (Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parsing plugin config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Validate enforces the per-plugin invariants at the host boundary. It is
// exported so plugin declarations assembled from another source (e.g. the
// server config file) can be validated without round-tripping through JSON.
func (c Config) Validate() error {
	seen := make(map[string]struct{}, len(c.Plugins))
	for i, p := range c.Plugins {
		switch {
		case p.Name == "":
			return fmt.Errorf("plugin[%d]: name is required", i)
		case len(p.Events) == 0:
			return fmt.Errorf("plugin %q: at least one event is required", p.Name)
		case p.Path == "" && p.Script == "":
			return fmt.Errorf("plugin %q: a path or an inline script is required", p.Name)
		case p.Path != "" && p.Script != "":
			return fmt.Errorf("plugin %q: set exactly one of path or script, not both", p.Name)
		}
		if _, dup := seen[p.Name]; dup {
			return fmt.Errorf("plugin %q: duplicate name", p.Name)
		}
		seen[p.Name] = struct{}{}
	}
	return nil
}
