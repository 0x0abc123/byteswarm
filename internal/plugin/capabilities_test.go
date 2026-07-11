package plugin

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/event"
)

func TestExecDenyByDefault(t *testing.T) {
	cap := NewExecCapability(ExecAllowlist{"backup": {"/usr/bin/tar", "czf"}})
	if !cap.Allowed("backup") {
		t.Fatal("allowlisted command reported as not allowed")
	}
	if _, err := cap.Run(context.Background(), "rm", []string{"-rf", "/"}); !errors.Is(err, ErrCommandDenied) {
		t.Fatalf("Run(non-allowlisted) error = %v, want ErrCommandDenied", err)
	}
}

// fakeRepo records the last key it saw so namespacing can be asserted.
type fakeRepo struct{ lastKey string }

func (r *fakeRepo) Load(_ context.Context, id string) ([]byte, error) {
	r.lastKey = id
	return nil, nil
}
func (r *fakeRepo) Save(_ context.Context, id string, _ []byte) error {
	r.lastKey = id
	return nil
}

func TestNamespacedStorePrefixesKeys(t *testing.T) {
	repo := &fakeRepo{}
	store := NewNamespacedStore(repo, "greet")
	if _, err := store.Load(context.Background(), "counter"); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if repo.lastKey != "greet:counter" {
		t.Fatalf("repo saw key %q, want %q", repo.lastKey, "greet:counter")
	}
}

func TestSandboxedFSConfinement(t *testing.T) {
	fs := NewSandboxedFS("/srv/plugins/greet")
	if _, err := fs.Resolve("state/counter.json"); err != nil {
		t.Fatalf("Resolve(in-sandbox) returned error: %v", err)
	}
	for _, bad := range []string{"../secret", "../../etc/passwd", "/etc/passwd"} {
		if _, err := fs.Resolve(bad); !errors.Is(err, ErrPathEscape) {
			t.Fatalf("Resolve(%q) error = %v, want ErrPathEscape", bad, err)
		}
	}
}

// fakePublisher records published events.
type fakePublisher struct{ got []event.Event }

func (p *fakePublisher) Publish(_ context.Context, e event.Event) error {
	p.got = append(p.got, e)
	return nil
}

func TestPublishCapabilityForwards(t *testing.T) {
	pub := &fakePublisher{}
	cap := NewPublishCapability(pub)
	if err := cap.Publish(context.Background(), event.Event{Type: "derived"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if len(pub.got) != 1 || pub.got[0].Type != "derived" {
		t.Fatalf("publisher got %+v, want one event of type %q", pub.got, "derived")
	}
}

func TestExecRunsAllowlistedCommand(t *testing.T) {
	echo, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo not available")
	}
	cap := NewExecCapability(ExecAllowlist{"say": {echo}})
	res, err := cap.Run(context.Background(), "say", []string{"hello"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(string(res.Stdout), "hello") {
		t.Fatalf("stdout = %q, want it to contain %q", res.Stdout, "hello")
	}
}

func TestExecNonZeroExitIsResultNotError(t *testing.T) {
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false not available")
	}
	cap := NewExecCapability(ExecAllowlist{"fail": {falseBin}})
	res, err := cap.Run(context.Background(), "fail", nil)
	if err != nil {
		t.Fatalf("Run returned a host error for a non-zero exit: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("exit code = 0, want non-zero")
	}
}

func TestExecArgBounds(t *testing.T) {
	echo, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo not available")
	}
	cap := NewExecCapability(ExecAllowlist{"say": {echo}})
	if _, err := cap.Run(context.Background(), "say", make([]string, maxExecArgs+1)); !errors.Is(err, ErrExecArgs) {
		t.Fatalf("Run(too many args) error = %v, want ErrExecArgs", err)
	}
}

func TestSandboxedFSReadWriteRoundTrip(t *testing.T) {
	fs := NewSandboxedFS(filepath.Join(t.TempDir(), "greet"))
	if err := fs.WriteFile("state/counter.json", []byte("42")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := fs.ReadFile("state/counter.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "42" {
		t.Fatalf("read %q, want %q", got, "42")
	}
}

func TestSandboxedFSWriteEscapeRejected(t *testing.T) {
	fs := NewSandboxedFS(filepath.Join(t.TempDir(), "greet"))
	if err := fs.WriteFile("../escape.txt", []byte("x")); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("WriteFile(../escape.txt) error = %v, want ErrPathEscape", err)
	}
}

func TestSandboxedFSTooLarge(t *testing.T) {
	fs := NewSandboxedFS(filepath.Join(t.TempDir(), "big"))
	if err := fs.WriteFile("f", make([]byte, maxSandboxFileBytes+1)); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("WriteFile(oversize) error = %v, want ErrFileTooLarge", err)
	}
}

func TestPublishRejectsInvalidEvents(t *testing.T) {
	pub := &fakePublisher{}
	cap := NewPublishCapability(pub)
	bad := []event.Event{
		{Type: ""},                      // empty type
		{Type: "bad type"},              // space is not a valid subject token
		{Type: "ok.dotted"},             // '.' is the subject separator
		{Type: "ok", WorkflowID: "wf*"}, // wildcard
		{Type: "ok", Payload: make([]byte, maxScriptPayloadByte+1)}, // oversize
	}
	for _, e := range bad {
		if err := cap.Publish(context.Background(), e); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("Publish(%+v) error = %v, want ErrInvalidEvent", e, err)
		}
	}
	if len(pub.got) != 0 {
		t.Fatalf("publisher received %d events, want 0 (all rejected)", len(pub.got))
	}
}
