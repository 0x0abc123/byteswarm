package jobrunner

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0x0abc123/byteswarm/internal/eventclient"
)

// fakePub records published events.
type fakePub struct {
	mu     sync.Mutex
	events []eventclient.Event
}

func (f *fakePub) Publish(_ context.Context, e eventclient.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func testDeps(t *testing.T, pub Publisher) (Deps, *bytes.Buffer) {
	t.Helper()
	var logbuf bytes.Buffer
	return Deps{
		Cfg: Config{WorkdirBase: t.TempDir()},
		Pub: pub,
		Log: slog.New(slog.NewJSONHandler(&logbuf, nil)),
	}, &logbuf
}

func run(t *testing.T, d Deps, job Job, src string) error {
	t.Helper()
	return d.Run(context.Background(), job, src)
}

func TestResolveContainment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.js"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(dir, "ok.js"); err != nil {
		t.Fatalf("Resolve(ok.js): %v", err)
	}
	for _, bad := range []string{"/etc/passwd", "../escape.js", "missing.js"} {
		if _, _, err := Resolve(dir, bad); err == nil {
			t.Errorf("Resolve(%q) = nil error, want failure", bad)
		}
	}
}

func TestPublishInheritsWorkflowAndBounds(t *testing.T) {
	pub := &fakePub{}
	d, _ := testDeps(t, pub)
	err := run(t, d, Job{ID: "j1", Name: "n", WorkflowID: "wfA"},
		`host.publish("job_done", "", { ok: true, id: job.id });`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	e := pub.events[0]
	if e.Type != "job_done" || e.WorkflowID != "wfA" {
		t.Errorf("event = type %q wf %q, want job_done/wfA (inherited)", e.Type, e.WorkflowID)
	}
	if !strings.Contains(string(e.Payload), `"id":"j1"`) {
		t.Errorf("payload = %s, want it to carry job.id", e.Payload)
	}
}

func TestPublishInvalidTypeThrows(t *testing.T) {
	pub := &fakePub{}
	d, _ := testDeps(t, pub)
	if err := run(t, d, Job{Name: "n"}, `host.publish("bad type", "", {});`); err == nil {
		t.Fatal("publishing an invalid event type should throw and fail the job")
	}
	if len(pub.events) != 0 {
		t.Errorf("invalid type must not publish; got %d", len(pub.events))
	}
}

func TestExecCapturesOutputAndCode(t *testing.T) {
	d, _ := testDeps(t, &fakePub{})
	// echo to stdout, exit 0
	if err := run(t, d, Job{Name: "n"},
		`var r = host.exec("/bin/sh", ["-c", "printf hello"]);
		 if (r.stdout !== "hello") throw new Error("stdout=" + r.stdout);
		 if (r.code !== 0) throw new Error("code=" + r.code);`); err != nil {
		t.Fatalf("exec run: %v", err)
	}
	// non-zero exit is data, not an error
	if err := run(t, d, Job{Name: "n"},
		`var r = host.exec("/bin/sh", ["-c", "exit 3"]);
		 if (r.code !== 3) throw new Error("code=" + r.code);`); err != nil {
		t.Fatalf("exec nonzero run: %v", err)
	}
	// no-opts call form must work
	if err := run(t, d, Job{Name: "n"}, `host.exec("/bin/sh", ["-c", "true"]);`); err != nil {
		t.Fatalf("exec no-opts run: %v", err)
	}
}

func TestFsWriteReadInWorkdir(t *testing.T) {
	d, _ := testDeps(t, &fakePub{})
	if err := run(t, d, Job{ID: "jfs", Name: "n"},
		`host.fs.write("out/result.txt", "42");
		 if (!host.fs.exists("out/result.txt")) throw new Error("missing");
		 if (host.fs.read("out/result.txt") !== "42") throw new Error("bad read");
		 var names = host.fs.list("out");
		 if (names.indexOf("result.txt") < 0) throw new Error("not listed");`); err != nil {
		t.Fatalf("fs run: %v", err)
	}
	// The file landed under the per-job workdir.
	if _, err := os.Stat(filepath.Join(d.Cfg.WorkdirBase, "jfs", "out", "result.txt")); err != nil {
		t.Errorf("expected file under workdir: %v", err)
	}
}

func TestHTTPRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()
	d, _ := testDeps(t, &fakePub{})
	if err := run(t, d, Job{Name: "n"},
		`var r = host.http.request({ method: "POST", url: "`+srv.URL+`", body: "ping" });
		 if (r.status !== 201) throw new Error("status=" + r.status);
		 if (r.body !== "pong") throw new Error("body=" + r.body);`); err != nil {
		t.Fatalf("http run: %v", err)
	}
}

func TestLogWritesStructured(t *testing.T) {
	d, logbuf := testDeps(t, &fakePub{})
	if err := run(t, d, Job{ID: "jL", Name: "logger"}, `host.log("warn", "heads up", { code: 7 });`); err != nil {
		t.Fatalf("log run: %v", err)
	}
	out := logbuf.String()
	if !strings.Contains(out, "heads up") || !strings.Contains(out, `"job":"logger"`) {
		t.Errorf("log output missing message/context: %s", out)
	}
}

func TestRunWallClockInterrupt(t *testing.T) {
	d, _ := testDeps(t, &fakePub{})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx, Job{Name: "loop"}, `while (true) {}`) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a runaway job should be interrupted by the wall-clock and return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not interrupt the runaway job")
	}
}

func TestRunJobPublishesFailedOnCrash(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "boom.js"), []byte(`throw new Error("boom");`), 0o600); err != nil {
		t.Fatal(err)
	}
	pub := &fakePub{}
	d := Deps{Cfg: Config{JobsDir: dir, WorkdirBase: t.TempDir()}, Pub: pub, Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))}
	if err := d.RunJob(context.Background(), Job{ID: "jb", Name: "boom.js", WorkflowID: "wf"}); err == nil {
		t.Fatal("RunJob on a throwing script should return an error")
	}
	if len(pub.events) != 1 || pub.events[0].Type != "job_failed" {
		t.Fatalf("expected one job_failed safety-net event, got %+v", pub.events)
	}
	if pub.events[0].WorkflowID != "wf" {
		t.Errorf("job_failed wf = %q, want wf", pub.events[0].WorkflowID)
	}
}
