package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version"}, &out); err != nil {
		t.Fatalf("run(version) returned error: %v", err)
	}
	if !strings.Contains(out.String(), "byteswarmctl") {
		t.Fatalf("run(version) output = %q, want it to name the binary", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"nope"}, &out); err == nil {
		t.Fatal("run(nope) should return an error for an unknown command")
	}
}
