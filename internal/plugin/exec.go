package plugin

import (
	"context"
	"errors"
)

// ErrCommandDenied is returned when a script asks to run a command whose
// logical name is not in the host allowlist. Callers log this as a security
// event (without payloads or secrets) per ADR-0008.
var ErrCommandDenied = errors.New("plugin: command not in exec allowlist")

// ExecAllowlist maps a logical command name to a fixed argv template. The
// template is the trusted, host-defined command line; script-supplied
// arguments are appended as a pure argv array, never interpolated into a shell
// string (ADR-0008: no `sh -c`, no interpolation).
type ExecAllowlist map[string][]string

// ExecResult is the outcome of a host-mediated command run.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExecCapability runs OS commands on the script's behalf, but only those on the
// host allowlist. It is the highest-risk capability, so it fails closed:
// unknown names are denied before any process is created.
type ExecCapability struct {
	allow ExecAllowlist
}

// NewExecCapability copies the allowlist so later mutation of the caller's map
// cannot widen the capability.
func NewExecCapability(allow ExecAllowlist) *ExecCapability {
	cp := make(ExecAllowlist, len(allow))
	for name, argv := range allow {
		cp[name] = append([]string(nil), argv...)
	}
	return &ExecCapability{allow: cp}
}

// Allowed reports whether a logical command name is on the allowlist.
func (c *ExecCapability) Allowed(name string) bool {
	_, ok := c.allow[name]
	return ok
}

// Run resolves name against the allowlist (deny-by-default) and executes the
// fixed argv template with args appended. The guard is real; the process
// launch (exec.CommandContext, argv-only, no shell) is attached with the goja
// runtime.
func (c *ExecCapability) Run(_ context.Context, name string, _ []string) (ExecResult, error) {
	if !c.Allowed(name) {
		return ExecResult{}, ErrCommandDenied
	}
	// TODO(code-migration): exec.CommandContext(ctx, template[0], append(template[1:], args...)...)
	// with a context timeout and OS-level limits on the child (ADR-0008).
	return ExecResult{}, ErrNotImplemented
}
