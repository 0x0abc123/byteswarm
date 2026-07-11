package plugin

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrPathEscape is returned when a script-supplied path would resolve outside
// its per-plugin sandbox directory. Callers log this as a security event per
// ADR-0008.
var ErrPathEscape = errors.New("plugin: path escapes sandbox")

// SandboxedFS is the script `fs` capability: file access confined to a
// per-plugin base directory (ADR-0008). Every path is resolved and checked to
// stay within base; absolute paths and ".." traversal are rejected.
type SandboxedFS struct {
	base string
}

// NewSandboxedFS confines file access to base (cleaned to an absolute-form
// lexical root for prefix checks).
func NewSandboxedFS(base string) *SandboxedFS {
	return &SandboxedFS{base: filepath.Clean(base)}
}

// Resolve maps a script-supplied relative path to a real path inside the
// sandbox, or returns ErrPathEscape. This lexical guard rejects absolute paths
// and any ".." that climbs above base. Symlink-escape resolution is enforced
// at open time by the real I/O attached with the goja runtime.
func (f *SandboxedFS) Resolve(name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", ErrPathEscape
	}
	full := filepath.Join(f.base, name) // Join cleans, collapsing ".."
	if full != f.base && !strings.HasPrefix(full, f.base+string(filepath.Separator)) {
		return "", ErrPathEscape
	}
	return full, nil
}

// ReadFile reads a confined file. The path guard is real; the read itself
// (with symlink-escape check) is attached with the goja runtime.
func (f *SandboxedFS) ReadFile(name string) ([]byte, error) {
	if _, err := f.Resolve(name); err != nil {
		return nil, err
	}
	// TODO(code-migration): open with O_NOFOLLOW-equivalent symlink guard, os.ReadFile.
	return nil, ErrNotImplemented
}

// WriteFile writes a confined file. The path guard is real; the write itself
// (with symlink-escape check) is attached with the goja runtime.
func (f *SandboxedFS) WriteFile(name string, _ []byte) error {
	if _, err := f.Resolve(name); err != nil {
		return err
	}
	// TODO(code-migration): symlink-guarded create/write within base.
	return ErrNotImplemented
}
