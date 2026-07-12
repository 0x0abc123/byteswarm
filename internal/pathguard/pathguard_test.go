package pathguard

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveWithinBase(t *testing.T) {
	base := filepath.Clean("/srv/jobs")
	got, err := Resolve(base, "sub/report.js")
	if err != nil {
		t.Fatalf("Resolve(in-base) error: %v", err)
	}
	if want := filepath.Join(base, "sub/report.js"); got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
	// A name that cleans back to base itself is allowed.
	if _, err := Resolve(base, "."); err != nil {
		t.Errorf("Resolve(\".\") error: %v", err)
	}
}

func TestResolveRejectsEscapes(t *testing.T) {
	base := filepath.Clean("/srv/jobs")
	for _, bad := range []string{
		"/etc/passwd",       // absolute
		"../secret",         // climbs above base
		"sub/../../secret",  // climbs above after cleaning
		"../jobs-evil/x.js", // sibling with a shared prefix string
	} {
		if _, err := Resolve(base, bad); !errors.Is(err, ErrEscape) {
			t.Errorf("Resolve(%q) error = %v, want ErrEscape", bad, err)
		}
	}
}

func TestWithin(t *testing.T) {
	base := "/srv/jobs"
	cases := map[string]bool{
		"/srv/jobs":      true,
		"/srv/jobs/a/b":  true,
		"/srv/jobs-evil": false, // shared prefix but not beneath
		"/srv":           false,
		"/srv/other":     false,
	}
	for p, want := range cases {
		if got := Within(p, base); got != want {
			t.Errorf("Within(%q, %q) = %v, want %v", p, base, got, want)
		}
	}
}
