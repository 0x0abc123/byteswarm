package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/0x0abc123/byteswarm/internal/eventclient"
	"github.com/0x0abc123/byteswarm/internal/jobrunner"
)

// workerEnv marks the re-exec'd, detached worker process. When set, runCmd
// skips daemonization and runs the job in-process; the launcher sets it on the
// child it spawns.
const workerEnv = "_BYTESWARM_JOB_WORKER"

// runCmd executes an operator-authored job by name (ADR-0013). By default it
// DAEMONIZES: it re-execs itself detached (new session, stdio → the job log)
// and returns immediately, so the launching plugin's host.exec returns within
// the plugin invocation timeout while the job runs on. --foreground skips
// detaching (for debugging); the wall-clock deadline applies either way.
func runCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		jobsDir      = fs.String("jobs-dir", "", "operator-controlled directory of job scripts (required)")
		socket       = fs.String("socket", defaultSocketPath(), "path to the byteswarm /events Unix domain socket")
		workflow     = fs.String("workflow", "", "workflowID host.publish events inherit when unset")
		jobID        = fs.String("job-id", "", "correlation id for this job run (required)")
		workdirBase  = fs.String("workdir-base", "", "base directory for per-job working directories")
		logDir       = fs.String("log-dir", "", "directory for per-job log files (required unless --foreground)")
		maxWallClock = fs.Duration("max-wall-clock", time.Hour, "kill the job after this wall-clock duration")
		foreground   = fs.Bool("foreground", false, "run in the foreground (do not daemonize) — for debugging")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	switch {
	case *jobsDir == "":
		return errors.New("run: --jobs-dir is required")
	case *jobID == "":
		return errors.New("run: --job-id is required")
	case len(rest) == 0:
		return errors.New("run: a job name is required")
	}

	// Launcher path: detach and return immediately. The child re-runs this same
	// command with workerEnv set, so it falls through to execution below.
	if !*foreground && os.Getenv(workerEnv) == "" {
		if *logDir == "" {
			return errors.New("run: --log-dir is required unless --foreground")
		}
		return daemonize(filepath.Join(*logDir, *jobID+".log"))
	}

	// Worker (or --foreground) path: run the job under the wall-clock deadline.
	ctx, cancel := context.WithTimeout(context.Background(), *maxWallClock)
	defer cancel()

	deps := jobrunner.Deps{
		Cfg: jobrunner.Config{JobsDir: *jobsDir, WorkdirBase: *workdirBase},
		Pub: eventclient.New(*socket),
		Log: slog.New(slog.NewJSONHandler(out, nil)),
	}
	job := jobrunner.Job{ID: *jobID, Name: rest[0], WorkflowID: *workflow, Args: rest[1:]}
	return deps.RunJob(ctx, job)
}

// daemonize re-execs this binary detached into a new session, with stdio
// redirected to the job log, so the launching process can exit while the job
// runs on. The child is marked with workerEnv so it executes rather than
// re-daemonizing. Unix-only (setsid), consistent with the runner's platform.
func daemonize(logPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("run: locating executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return fmt.Errorf("run: log dir: %w", err)
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("run: opening job log: %w", err)
	}
	defer func() { _ = logf.Close() }()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("run: /dev/null: %w", err)
	}
	defer func() { _ = devnull.Close() }()

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), workerEnv+"=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach into a new session
	return cmd.Start()                                   // do not Wait: the launcher returns now
}

// defaultSocketPath mirrors byteswarmctl: the /events socket from
// BYTESWARM_EVENTS_SOCKET, else the server's default relative path (ADR-0011).
func defaultSocketPath() string {
	if p := os.Getenv("BYTESWARM_EVENTS_SOCKET"); p != "" {
		return p
	}
	return "byteswarm-events.sock"
}
