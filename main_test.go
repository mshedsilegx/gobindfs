package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRunCommandExecution(t *testing.T) {
	deps := mockSystemDeps()
	var buf bytes.Buffer

	// Test Version
	exitCode := run([]string{"-version"}, deps, &buf)
	if exitCode != 0 {
		t.Errorf("Expected 0, got %d", exitCode)
	}

	if !strings.Contains(buf.String(), "gobindfs version") {
		t.Errorf("Expected version string, got %s", buf.String())
	}

	// Test Unmount Success
	buf.Reset()
	exitCode = run([]string{"-umount", "/tmp/fake"}, deps, &buf)
	if exitCode != 0 {
		t.Errorf("Expected 0 on unmount success, got %d", exitCode)
	}

	// Test Unmount All Success
	buf.Reset()
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte(""), nil // No mounts
		}
		return nil, os.ErrNotExist
	}
	exitCode = run([]string{"-umount-all"}, deps, &buf)
	if exitCode != 0 {
		t.Errorf("Expected 0 on umount-all success, got %d", exitCode)
	}

	deps.ExecCombinedOutput = func(name string, arg ...string) ([]byte, error) {
		return nil, fmt.Errorf("mock fusermount failure")
	}
	// Test Unmount Failure (Syscall error)
	deps.SyscallUnmount = func(target string, flags int) error {
		return fmt.Errorf("mock failure")
	}
	buf.Reset()
	exitCode = run([]string{"-umount", "/tmp/fake"}, deps, &buf)
	if exitCode != 1 {
		t.Errorf("Expected 1 on unmount failure, got %d", exitCode)
	}
	deps.SyscallUnmount = func(target string, flags int) error { return nil }

	// Test listMounts Error
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return nil, fmt.Errorf("mock error")
		}
		return nil, os.ErrNotExist
	}
	buf.Reset()
	exitCode = run([]string{"-listmounts"}, deps, &buf)
	if exitCode != 1 {
		t.Errorf("Expected 1 on listMounts failure, got %d", exitCode)
	}

	// Test invalid flags
	buf.Reset()
	exitCode = run([]string{"-invalidflag"}, deps, &buf)
	if exitCode != 1 {
		t.Errorf("Expected 1 on parse failure, got %d", exitCode)
	}
}
