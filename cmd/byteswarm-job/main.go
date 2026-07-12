// Command byteswarm-job is the contained goja job-runner (ADR-0013): a third
// static binary that runs operator-authored, long-running JavaScript jobs,
// triggered by name through the plugin exec allowlist and running outside the
// plugin sandbox. It self-daemonizes so the launching plugin invocation returns
// immediately, then reports completion by publishing to the server's /events
// socket (internal/eventclient).
//
// PR 2 (refactor-0005) adds the `run` subcommand: resolve a job by name and
// execute it in the foreground with the full goja host API (job/publish/exec/
// fs/http/log, internal/jobrunner). Daemonization (setsid/re-exec ahead of the
// run so the launching plugin returns immediately) and the wall-clock watchdog
// land in PR 3; --foreground will then select the non-detaching path.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// version is the runner build version, overridable at link time via -ldflags
// (-X main.version=...), matching byteswarm and byteswarmctl.
var version = "0.0.0-dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "byteswarm-job:", err)
		os.Exit(1)
	}
}

// run is the testable entry point: it parses flags and dispatches. Job
// execution is added in refactor-0005 PR 2.
func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("byteswarm-job", flag.ContinueOnError)
	fs.SetOutput(out)
	// foreground is parsed now so the flag contract is stable; the daemonize
	// path that consumes it lands with job execution.
	foreground := fs.Bool("foreground", false, "run in the foreground (do not daemonize) — for debugging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = foreground

	switch cmd := fs.Arg(0); cmd {
	case "", "version":
		fmt.Fprintf(out, "byteswarm-job %s\n", version)
		return nil
	case "run":
		return runCmd(fs.Args()[1:], out)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}
