// Package pathguard is the shared lexical containment check: a supplied name
// must resolve to a path at or beneath a fixed base directory. It rejects
// absolute paths and any ".." that climbs above base. It is the single source
// of the "resolve within a root" rule used by the plugin filesystem sandbox
// (internal/plugin) and the job-runner's job-name resolution (ADR-0008/0013).
//
// pathguard is purely lexical. Callers that must also defeat symlink escapes
// resolve the real path (filepath.EvalSymlinks) and re-check it with Within.
package pathguard

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrEscape is returned when a supplied name would resolve outside base.
var ErrEscape = errors.New("path escapes base")

// Resolve maps a relative name to a cleaned path inside base, or returns
// ErrEscape. Absolute names, and any ".." that climbs above base, are rejected.
// base should already be cleaned by the caller (e.g. filepath.Clean at
// construction).
func Resolve(base, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", ErrEscape
	}
	full := filepath.Join(base, name) // Join cleans, collapsing ".."
	if !Within(full, base) {
		return "", ErrEscape
	}
	return full, nil
}

// Within reports whether p is base or lies beneath it.
func Within(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}
