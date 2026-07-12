package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCmdExecutesJob(t *testing.T) {
	jobsDir := t.TempDir()
	workdir := t.TempDir()
	// A job that only touches the filesystem — it never publishes, so no
	// /events socket is needed for this foreground run.
	script := `host.fs.write("done.txt", "ok");`
	if err := os.WriteFile(filepath.Join(jobsDir, "hello.js"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runCmd([]string{
		"--foreground", // run inline rather than daemonizing
		"--jobs-dir", jobsDir,
		"--job-id", "j1",
		"--workdir-base", workdir,
		"hello.js",
	}, &out)
	if err != nil {
		t.Fatalf("runCmd: %v (out=%s)", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(workdir, "j1", "done.txt")); err != nil {
		t.Errorf("job output not written under workdir: %v", err)
	}
}

func TestRunCmdRequiresFlags(t *testing.T) {
	cases := [][]string{
		{"--job-id", "j1", "hello.js"},                            // missing --jobs-dir
		{"--jobs-dir", t.TempDir(), "hello.js"},                   // missing --job-id
		{"--jobs-dir", t.TempDir(), "--job-id", "j1"},             // missing job name
		{"--jobs-dir", t.TempDir(), "--job-id", "j1", "hello.js"}, // not --foreground, no --log-dir
	}
	for _, args := range cases {
		var out bytes.Buffer
		if err := runCmd(args, &out); err == nil {
			t.Errorf("runCmd(%v) = nil error, want failure", args)
		}
	}
}
