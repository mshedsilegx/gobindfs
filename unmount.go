package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"syscall"
)

// doUnmount wraps the execution process to cleanly unmount a specific mountpoint.
//
// The process uses a tiered approach to ensure stability across distributions:
//  1. It tries to execute `fusermount3 -u` which handles FUSE mounts in modern distros.
//  2. If that fails, it falls back to the legacy `fusermount -u`.
//  3. If both FUSE binaries are absent or fail, it makes a last resort low-level
//     system call via `syscall.Unmount`.
//
// The target path is cleaned and made absolute to prevent shell execution ambiguity.
func doUnmount(mp string, deps *SystemDeps) error {
	absMp, err := filepath.Abs(filepath.Clean(mp))
	if err != nil {
		return fmt.Errorf("invalid mountpoint %q: %w", mp, err)
	}
	out, err := deps.ExecCombinedOutput("fusermount3", "-u", absMp)
	if err != nil {
		out2, err2 := deps.ExecCombinedOutput("fusermount", "-u", absMp)
		if err2 != nil {
			// Fallback to standard syscall.Unmount if both fail
			if errSys := deps.SyscallUnmount(absMp, 0); errSys != nil {
				errMsg := fmt.Sprintf("fusermount3 failed: %v (%s), fusermount failed: %v (%s), syscall unmount also failed: %v", err, strings.TrimSpace(string(out)), err2, strings.TrimSpace(string(out2)), errSys)
				if strings.Contains(errMsg, "Device or resource busy") || errSys == syscall.EBUSY {
					return fmt.Errorf("mountpoint %q is locked or busy: %w", mp, errSys)
				}
				return fmt.Errorf("%s", errMsg)
			}
		}
	}
	return nil
}

// doUnmountAll acts as a highly-reliable cleanup mechanism.
// It parses the system's /proc/mounts to identify all active filesystems
// previously established by the current binary, and forcefully detaches them.
func doUnmountAll(deps *SystemDeps, out io.Writer) error {
	activeMounts, err := getActiveMounts(deps)
	if err != nil {
		return fmt.Errorf("failed to retrieve active mounts: %w", err)
	}

	if len(activeMounts) == 0 {
		_, _ = fmt.Fprintln(out, "No active gobindfs mounts found.")
		return nil
	}

	var errs []string
	for _, m := range activeMounts {
		if err := doUnmount(m.Mountpoint, deps); err != nil {
			errs = append(errs, fmt.Sprintf("Failed to unmount %s: %v", m.Mountpoint, err))
		} else {
			_, _ = fmt.Fprintf(out, "Unmounted %s\n", m.Mountpoint)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("unmount all encountered errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
