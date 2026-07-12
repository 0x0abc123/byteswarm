// Package jobrunner is the host adapter for byteswarm-job (ADR-0013): it
// resolves an operator-authored job script by name within the operator-
// controlled jobs directory (fail-closed containment via internal/pathguard),
// then runs it on a goja runtime with a deliberately broad host API — publish
// (to /events via internal/eventclient), exec, fs, http, log — that sits
// OUTSIDE the plugin sandbox. It excludes host.store and any direct bus access.
//
// Phase 1 (this package) provides the host API and in-process ("foreground")
// execution. Daemonization and the wall-clock watchdog are wired in the
// byteswarm-job composition root.
package jobrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dop251/goja"

	"github.com/0x0abc123/byteswarm/internal/eventclient"
	"github.com/0x0abc123/byteswarm/internal/pathguard"
)

// Config bounds and locates a job run. Zero-valued size fields take defaults.
type Config struct {
	JobsDir        string // operator-controlled directory holding job scripts
	WorkdirBase    string // per-job workdir = WorkdirBase/<job.ID>; empty disables relative fs
	MaxFileBytes   int64  // host.fs single read/write cap
	MaxOutputBytes int    // host.exec captured stdout/stderr cap (each stream)
	HTTPMaxBytes   int64  // host.http.request response-body cap
}

const (
	defaultMaxFileBytes   = 64 << 20 // 64 MiB
	defaultMaxOutputBytes = 1 << 20  // 1 MiB
	defaultHTTPMaxBytes   = 8 << 20  // 8 MiB
	maxPublishPayload     = 1 << 20  // 1 MiB — mirrors the plugin publish bound (ADR-0010)
	maxWorkflowIDLen      = 128
)

func (c Config) withDefaults() Config {
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = defaultMaxFileBytes
	}
	if c.MaxOutputBytes <= 0 {
		c.MaxOutputBytes = defaultMaxOutputBytes
	}
	if c.HTTPMaxBytes <= 0 {
		c.HTTPMaxBytes = defaultHTTPMaxBytes
	}
	return c
}

// Job is the read-only invocation context a job script sees as `job`. Args is
// UNTRUSTED — it is supplied by the triggering plugin, which processed an
// untrusted event payload; job scripts must validate it (ADR-0013).
type Job struct {
	ID         string
	Name       string
	WorkflowID string
	Args       []string
}

// Publisher submits events to the server's /events ingress. *eventclient.Client
// satisfies it; tests inject a fake.
type Publisher interface {
	Publish(ctx context.Context, e eventclient.Event) error
}

// Deps are the runner's injected dependencies (composition-root wired).
type Deps struct {
	Cfg Config
	Pub Publisher
	Log *slog.Logger
}

// Resolve maps a job name to its script inside the operator-controlled jobs
// directory. It fails closed (pathguard): absolute paths and ".." escapes are
// rejected, and the target must be an existing regular file. A plugin supplies
// only the name, so this is the containment boundary that keeps the runner from
// executing arbitrary paths (ADR-0013).
func Resolve(jobsDir, name string) (path string, src []byte, err error) {
	full, err := pathguard.Resolve(filepath.Clean(jobsDir), name)
	if err != nil {
		return "", nil, fmt.Errorf("job %q: %w", name, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", nil, fmt.Errorf("job %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("job %q: not a regular file", name)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", nil, fmt.Errorf("job %q: %w", name, err)
	}
	return full, b, nil
}

// RunJob resolves the job by name and runs it. On resolution or run failure it
// logs and publishes a job_failed safety-net event (ADR-0013) — so a job that
// dies without reporting is still visible — then returns the error.
func (d Deps) RunJob(ctx context.Context, job Job) error {
	_, src, err := Resolve(d.Cfg.JobsDir, job.Name)
	if err != nil {
		d.failed(ctx, job, err)
		return err
	}
	if err := d.Run(ctx, job, string(src)); err != nil {
		d.failed(ctx, job, err)
		return err
	}
	return nil
}

// Run executes a job script on a fresh goja runtime with the host/job API
// bound. It is exported so tests can run an inline script.
func (d Deps) Run(ctx context.Context, job Job, src string) error {
	d.Cfg = d.Cfg.withDefaults()
	workdir := filepath.Join(d.Cfg.WorkdirBase, job.ID)
	if d.Cfg.WorkdirBase != "" {
		if err := os.MkdirAll(workdir, 0o750); err != nil {
			return fmt.Errorf("job %q: workdir: %w", job.Name, err)
		}
	}
	prog, err := goja.Compile(job.Name, src, true)
	if err != nil {
		return fmt.Errorf("job %q: compile: %w", job.Name, err)
	}
	rt := goja.New()
	(&binding{ctx: ctx, deps: d, job: job, workdir: workdir}).inject(rt)

	// Wall-clock watchdog: interrupt the VM when ctx is cancelled or its
	// deadline passes, so a runaway job (tight loop, wedged native call) is
	// actually stopped. goja cannot preempt otherwise (cf. the plugin host).
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			rt.Interrupt(ctx.Err())
		case <-stop:
		}
	}()
	defer close(stop)

	if _, err := rt.RunProgram(prog); err != nil {
		return fmt.Errorf("job %q: %w", job.Name, err)
	}
	return nil
}

// failed logs and best-effort publishes the job_failed safety-net event.
func (d Deps) failed(ctx context.Context, job Job, cause error) {
	if d.Log != nil {
		d.Log.Error("job failed", "job", job.Name, "jobId", job.ID, "err", cause.Error())
	}
	if d.Pub == nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{"jobId": job.ID, "job": job.Name, "error": cause.Error()})
	_ = d.Pub.Publish(ctx, eventclient.Event{Type: "job_failed", WorkflowID: job.WorkflowID, Payload: payload})
}
