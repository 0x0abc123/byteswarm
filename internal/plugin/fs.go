package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathEscape is returned when a script-supplied path would resolve outside
// its per-plugin sandbox directory. Callers log this as a security event per
// ADR-0008.
var ErrPathEscape = errors.New("plugin: path escapes sandbox")

// ErrFileTooLarge is returned when a read or write exceeds the sandbox file
// size bound (reference/security-fundamentals.md: bound all input).
var ErrFileTooLarge = errors.New("plugin: file exceeds sandbox size limit")

// maxSandboxFileBytes bounds a single sandboxed read or write.
const maxSandboxFileBytes = 8 << 20 // 8 MiB

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

// realBase ensures the sandbox directory exists and returns its symlink-
// resolved real path, used to re-check containment after symlink resolution.
func (f *SandboxedFS) realBase() (string, error) {
	if err := os.MkdirAll(f.base, 0o750); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(f.base)
}

// within reports whether real is base or lies beneath it.
func within(real, base string) bool {
	return real == base || strings.HasPrefix(real, base+string(filepath.Separator))
}

// ReadFile reads a confined file. Beyond the lexical guard in Resolve, the
// real (symlink-resolved) path is re-checked against the real sandbox root, so
// a symlink planted inside the sandbox cannot read outside it.
func (f *SandboxedFS) ReadFile(name string) ([]byte, error) {
	full, err := f.Resolve(name)
	if err != nil {
		return nil, err
	}
	base, err := f.realBase()
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, err
	}
	if !within(real, base) {
		return nil, ErrPathEscape
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSandboxFileBytes {
		return nil, ErrFileTooLarge
	}
	return os.ReadFile(real)
}

// WriteFile writes a confined file, creating parent directories within the
// sandbox. The parent directory's real path is re-checked after symlink
// resolution so a symlinked directory cannot redirect the write outside base.
func (f *SandboxedFS) WriteFile(name string, data []byte) error {
	if len(data) > maxSandboxFileBytes {
		return ErrFileTooLarge
	}
	full, err := f.Resolve(name)
	if err != nil {
		return err
	}
	base, err := f.realBase()
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if !within(realDir, base) {
		return ErrPathEscape
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return fmt.Errorf("plugin sandbox write: %w", err)
	}
	return nil
}
