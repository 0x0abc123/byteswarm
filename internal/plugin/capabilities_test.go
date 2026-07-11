package plugin

import (
	"context"
	"errors"
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
