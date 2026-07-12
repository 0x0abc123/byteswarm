package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
)

// ErrCommandDenied is returned when a script asks to run a command whose
// logical name is not in the host allowlist. Callers log this as a security
// event (without payloads or secrets) per ADR-0008.
var ErrCommandDenied = errors.New("plugin: command not in exec allowlist")

// ErrExecArgs is returned when script-supplied arguments exceed the host
// bounds. All external input is bounded at the boundary
// (reference/security-fundamentals.md).
var ErrExecArgs = errors.New("plugin: too many or too large exec arguments")

// Argument bounds for a single script-supplied exec call.
const (
	maxExecArgs   = 64
	maxExecArgLen = 4096
)

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

// AllowedCommands returns the logical command names the plugin may exec, sorted
// for a stable order. It exposes only the names a script passes to host.exec —
// never the argv templates, which would leak host binary paths and fixed
// arguments without giving the script anything it can act on.
func (c *ExecCapability) AllowedCommands() []string {
	names := make([]string, 0, len(c.allow))
	for name := range c.allow {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Run resolves name against the allowlist (deny-by-default) and executes the
// fixed argv template with the script-supplied args appended as a pure argv
// array. There is no shell: the binary is invoked directly (ADR-0008: no
// `sh -c`, no interpolation), so script arguments cannot inject a command. A
// non-zero exit is a normal result reported in ExitCode, not a host error;
// only a failure to launch returns an error. The invocation context carries
// the per-invocation deadline, so a runaway child is killed with the script.
func (c *ExecCapability) Run(ctx context.Context, name string, args []string) (ExecResult, error) {
	tmpl, ok := c.allow[name]
	if !ok || len(tmpl) == 0 {
		return ExecResult{}, ErrCommandDenied
	}
	if len(args) > maxExecArgs {
		return ExecResult{}, ErrExecArgs
	}
	for _, a := range args {
		if len(a) > maxExecArgLen {
			return ExecResult{}, ErrExecArgs
		}
	}

	argv := append(append([]string(nil), tmpl[1:]...), args...)
	cmd := exec.CommandContext(ctx, tmpl[0], argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	res := ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// The command ran and exited non-zero: that is data for the
			// script (ExitCode), not a host-side failure.
			return res, nil
		}
		return res, fmt.Errorf("plugin exec %q: %w", name, err)
	}
	return res, nil
}
