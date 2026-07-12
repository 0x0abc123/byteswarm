package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"

	"github.com/0x0abc123/byteswarm/internal/eventclient"
	"github.com/0x0abc123/byteswarm/internal/jobrunner"
)

// runCmd executes an operator-authored job by name. This is the foreground
// path (refactor-0005 PR 2): the launching plugin's host.exec blocks until it
// returns. Daemonization (setsid/re-exec so the launcher returns immediately)
// and the wall-clock watchdog are added in PR 3, ahead of this call.
func runCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		jobsDir     = fs.String("jobs-dir", "", "operator-controlled directory of job scripts (required)")
		socket      = fs.String("socket", defaultSocketPath(), "path to the byteswarm /events Unix domain socket")
		workflow    = fs.String("workflow", "", "workflowID host.publish events inherit when unset")
		jobID       = fs.String("job-id", "", "correlation id for this job run (required)")
		workdirBase = fs.String("workdir-base", "", "base directory for per-job working directories")
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

	deps := jobrunner.Deps{
		Cfg: jobrunner.Config{JobsDir: *jobsDir, WorkdirBase: *workdirBase},
		Pub: eventclient.New(*socket),
		Log: slog.New(slog.NewJSONHandler(out, nil)),
	}
	job := jobrunner.Job{ID: *jobID, Name: rest[0], WorkflowID: *workflow, Args: rest[1:]}
	return deps.RunJob(context.Background(), job)
}

// defaultSocketPath mirrors byteswarmctl: the /events socket from
// BYTESWARM_EVENTS_SOCKET, else the server's default relative path (ADR-0011).
func defaultSocketPath() string {
	if p := os.Getenv("BYTESWARM_EVENTS_SOCKET"); p != "" {
		return p
	}
	return "byteswarm-events.sock"
}
