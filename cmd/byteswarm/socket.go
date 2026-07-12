package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// listenUnix binds the operator-local /events Unix domain socket with
// defence-in-depth file permissions (ADR-0011): the socket's permissions are
// the access control, not the network. It unlinks a stale socket left by a
// crash first (refusing to clobber a non-socket), creates the socket under a
// restrictive umask so it never briefly exists group- or world-reachable,
// assigns the operator group if configured, then widens the mode to the
// configured value — group access is granted only after the group owns it. The
// returned listener unlinks the socket on Close (net.Listen's default), so
// graceful shutdown removes it.
func listenUnix(cfg socketConfig) (net.Listener, error) {
	mode, err := cfg.parseMode()
	if err != nil {
		return nil, fmt.Errorf("socket mode: %w", err)
	}

	if info, err := os.Lstat(cfg.Path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to bind: %q exists and is not a socket", cfg.Path)
		}
		if err := os.Remove(cfg.Path); err != nil {
			return nil, fmt.Errorf("removing stale socket %q: %w", cfg.Path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat socket %q: %w", cfg.Path, err)
	}

	// umask 0177 => the socket is created at most 0600 (owner only), closing the
	// window where a client could connect before ownership/mode are fixed.
	old := syscall.Umask(0o177)
	ln, err := net.Listen("unix", cfg.Path)
	syscall.Umask(old)
	if err != nil {
		return nil, fmt.Errorf("listening on unix socket %q: %w", cfg.Path, err)
	}

	if cfg.Group != "" {
		gid, err := lookupGID(cfg.Group)
		if err != nil {
			_ = ln.Close()
			return nil, err
		}
		if err := os.Chown(cfg.Path, -1, gid); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("chown socket %q to group %q: %w", cfg.Path, cfg.Group, err)
		}
	}

	if err := os.Chmod(cfg.Path, mode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("chmod socket %q to %v: %w", cfg.Path, mode, err)
	}
	return ln, nil
}

// lookupGID resolves a group name to its numeric GID.
func lookupGID(group string) (int, error) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, fmt.Errorf("resolving socket group %q: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("group %q has non-numeric gid %q: %w", group, g.Gid, err)
	}
	return gid, nil
}
