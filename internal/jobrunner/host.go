package jobrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/0x0abc123/byteswarm/internal/event"
	"github.com/0x0abc123/byteswarm/internal/eventclient"
)

// binding holds the per-invocation state the host shims close over.
type binding struct {
	ctx     context.Context
	deps    Deps
	job     Job
	workdir string
}

// inject binds the read-only `job` context and the `host` capability object.
// A shim that returns a non-nil error surfaces to the script as a thrown
// exception, failing the job (and, via RunJob, the job_failed safety net).
func (b *binding) inject(rt *goja.Runtime) {
	_ = rt.Set("job", map[string]interface{}{
		"id":         b.job.ID,
		"name":       b.job.Name,
		"workflowID": b.job.WorkflowID,
		"args":       b.job.Args,
	})
	_ = rt.Set("host", map[string]interface{}{
		"publish": b.publish,
		"exec":    b.exec,
		"fs": map[string]interface{}{
			"read":    b.fsRead,
			"write":   b.fsWrite,
			"append":  b.fsAppend,
			"exists":  b.fsExists,
			"list":    b.fsList,
			"mkdir":   b.fsMkdir,
			"remove":  b.fsRemove,
			"rename":  b.fsRename,
			"workdir": func() string { return b.workdir },
		},
		"http": map[string]interface{}{
			"request": b.httpRequest,
		},
		"log": b.log,
	})
}

// publish emits an event to /events (via eventclient). type is validated
// (ADR-0010) and payload is bounded, mirroring the plugin publish capability;
// an empty workflowID inherits the job's.
func (b *binding) publish(typ, workflowID string, payload interface{}) error {
	if !event.ValidType(typ) {
		return fmt.Errorf("host.publish: invalid event type %q", typ)
	}
	wf := workflowID
	if wf == "" {
		wf = b.job.WorkflowID
	}
	if len(wf) > maxWorkflowIDLen {
		return fmt.Errorf("host.publish: workflowID exceeds %d chars", maxWorkflowIDLen)
	}
	var pb []byte
	if payload != nil {
		p, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("host.publish: payload: %w", err)
		}
		if len(p) > maxPublishPayload {
			return fmt.Errorf("host.publish: payload exceeds %d bytes", maxPublishPayload)
		}
		pb = p
	}
	return b.deps.Pub.Publish(b.ctx, eventclient.Event{Type: typ, WorkflowID: wf, Payload: pb})
}

// exec runs an OS command with unrestricted argv (no shell, no allowlist —
// the job is operator-authored, ADR-0013). A non-zero exit is data in `code`,
// not an error; only a failure to launch throws.
func (b *binding) exec(bin string, args []string, opts map[string]interface{}) (map[string]interface{}, error) {
	ctx := b.ctx
	if ms := optInt(opts, "timeoutMs", 0); ms > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(b.ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if cwd := optString(opts, "cwd", ""); cwd != "" {
		cmd.Dir = cwd
	}
	if env := optStringMap(opts, "env"); len(env) > 0 {
		e := os.Environ()
		for k, v := range env {
			e = append(e, k+"="+v)
		}
		cmd.Env = e
	}
	if stdin := optString(opts, "stdin", ""); stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	max := optInt(opts, "maxOutputBytes", b.deps.Cfg.MaxOutputBytes)
	stdout, stderr := &capBuf{max: max}, &capBuf{max: max}
	cmd.Stdout, cmd.Stderr = stdout, stderr

	err := cmd.Run()
	res := map[string]interface{}{"stdout": stdout.b.String(), "stderr": stderr.b.String(), "code": 0}
	if cmd.ProcessState != nil {
		res["code"] = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return res, nil // ran and exited non-zero: data, not a host error
		}
		return res, fmt.Errorf("host.exec %q: %w", bin, err)
	}
	return res, nil
}

// fsPath resolves a script-supplied path: absolute paths are used as-is (the
// runner fs is intentionally open, ADR-0013), relative paths root at the
// per-job workdir.
func (b *binding) fsPath(name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(b.workdir, name)
}

func (b *binding) fsRead(name string) (string, error) {
	p := b.fsPath(name)
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if info.Size() > b.deps.Cfg.MaxFileBytes {
		return "", fmt.Errorf("host.fs.read: %q exceeds %d bytes", name, b.deps.Cfg.MaxFileBytes)
	}
	data, err := os.ReadFile(p)
	return string(data), err
}

func (b *binding) fsWrite(name, data string) error  { return b.fsPut(name, data, false) }
func (b *binding) fsAppend(name, data string) error { return b.fsPut(name, data, true) }

func (b *binding) fsPut(name, data string, appendMode bool) error {
	if int64(len(data)) > b.deps.Cfg.MaxFileBytes {
		return fmt.Errorf("host.fs: %q exceeds %d bytes", name, b.deps.Cfg.MaxFileBytes)
	}
	p := b.fsPath(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(p, flag, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(data)
	return err
}

func (b *binding) fsExists(name string) bool {
	_, err := os.Stat(b.fsPath(name))
	return err == nil
}

func (b *binding) fsList(name string) ([]string, error) {
	entries, err := os.ReadDir(b.fsPath(name))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

func (b *binding) fsMkdir(name string) error  { return os.MkdirAll(b.fsPath(name), 0o750) }
func (b *binding) fsRemove(name string) error { return os.Remove(b.fsPath(name)) }
func (b *binding) fsRename(oldName, newName string) error {
	return os.Rename(b.fsPath(oldName), b.fsPath(newName))
}

// httpRequest performs an HTTP(S) request with open egress (ADR-0013). The
// response body is bounded; the per-request timeout derives from the job ctx.
func (b *binding) httpRequest(opts map[string]interface{}) (map[string]interface{}, error) {
	url := optString(opts, "url", "")
	if url == "" {
		return nil, errors.New("host.http.request: url is required")
	}
	timeout := time.Duration(optInt(opts, "timeoutMs", 30000)) * time.Millisecond
	maxBytes := int64(optInt(opts, "maxBytes", int(b.deps.Cfg.HTTPMaxBytes)))
	ctx, cancel := context.WithTimeout(b.ctx, timeout)
	defer cancel()

	var body io.Reader
	if s := optString(opts, "body", ""); s != "" {
		body = strings.NewReader(s)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(optString(opts, "method", http.MethodGet)), url, body)
	if err != nil {
		return nil, fmt.Errorf("host.http.request: %w", err)
	}
	for k, v := range optStringMap(opts, "headers") {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("host.http.request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	headers := make(map[string]interface{}, len(resp.Header))
	for k, v := range resp.Header {
		headers[k] = v
	}
	return map[string]interface{}{"status": resp.StatusCode, "headers": headers, "body": string(data)}, nil
}

// log writes a structured line to the job logger — the detached job's only
// console. Uncaught script errors are reported separately by RunJob.
func (b *binding) log(level, msg string, fields map[string]interface{}) {
	if b.deps.Log == nil {
		return
	}
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	attrs := []any{"job", b.job.Name, "jobId", b.job.ID}
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	b.deps.Log.Log(b.ctx, lvl, msg, attrs...)
}

// --- option helpers: JS objects arrive as map[string]interface{} ---

func optString(m map[string]interface{}, key, def string) string {
	if m != nil {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return def
}

func optInt(m map[string]interface{}, key string, def int) int {
	if m != nil {
		switch v := m[key].(type) {
		case int64:
			return int(v)
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return def
}

func optStringMap(m map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	if m == nil {
		return out
	}
	sub, ok := m[key].(map[string]interface{})
	if !ok {
		return out
	}
	for k, v := range sub {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// capBuf is a bytes.Buffer bounded to max bytes; overflow is discarded (not an
// error), so a chatty command cannot exhaust memory.
type capBuf struct {
	b   bytes.Buffer
	max int
}

func (c *capBuf) Write(p []byte) (int, error) {
	if c.max > 0 {
		room := c.max - c.b.Len()
		if room <= 0 {
			return len(p), nil
		}
		if len(p) > room {
			c.b.Write(p[:room])
			return len(p), nil
		}
	}
	return c.b.Write(p)
}
