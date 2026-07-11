// Command byteswarmctl is the byteswarm CLI — the primary operator client
// (ADR-0002). It parses subcommands with the stdlib flag package (no CLI
// framework, per ADR-0002/ADR-0003) and produces events and commands to the
// server's ingress. Today it exposes only `version`; further subcommands are
// added as features land.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// version is the CLI build version, overridable at link time via -ldflags.
var version = "0.0.0-dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "byteswarmctl:", err)
		os.Exit(1)
	}
}

// run is the testable entry point: it parses args and dispatches subcommands,
// writing normal output to out.
func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("byteswarmctl", flag.ContinueOnError)
	fs.SetOutput(out)
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch cmd := fs.Arg(0); cmd {
	case "", "version":
		fmt.Fprintf(out, "byteswarmctl %s\n", version)
		return nil
	case "publish":
		return publishCmd(fs.Args()[1:], out)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}
