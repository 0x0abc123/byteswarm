package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version"}, &out); err != nil {
		t.Fatalf("run(version): %v", err)
	}
	if !strings.Contains(out.String(), "byteswarm-job ") {
		t.Errorf("version output = %q, want it to name the binary", out.String())
	}
}

func TestRunDefaultsToVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err != nil {
		t.Fatalf("run(nil): %v", err)
	}
	if !strings.Contains(out.String(), "byteswarm-job ") {
		t.Errorf("default output = %q, want version", out.String())
	}
}

func TestRunUnknownCommandErrors(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"frobnicate"}, &out); err == nil {
		t.Fatal("run(unknown) should return an error")
	}
}

func TestRunForegroundFlagParses(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--foreground", "version"}, &out); err != nil {
		t.Fatalf("run(--foreground version): %v", err)
	}
}
