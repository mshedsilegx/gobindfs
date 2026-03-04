package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDoUnmount_Fallback(t *testing.T) {
	deps := mockSystemDeps()
	// Mock fusermount3 and fusermount failures
	deps.ExecCombinedOutput = func(name string, arg ...string) ([]byte, error) {
		return []byte("command not found"), os.ErrNotExist
	}

	syscallCalled := false
	deps.SyscallUnmount = func(target string, flags int) error {
		syscallCalled = true
		return nil
	}

	err := doUnmount("/tmp/fake", deps)
	if err != nil {
		t.Fatalf("Expected success with syscall fallback, got %v", err)
	}

	if !syscallCalled {
		t.Errorf("Expected syscall.Unmount to be called")
	}
}

func TestDoUnmountAll(t *testing.T) {
	deps := mockSystemDeps()
	mockProcMounts := `
/dev/loop0 /mnt/fake1 fuse.gobindfs rw 0 0
/dev/loop1 /mnt/fake2 fuse.gobindfs rw 0 0
`
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte(mockProcMounts), nil
		}
		return nil, os.ErrNotExist
	}

	unmounted := make([]string, 0)
	deps.ExecCombinedOutput = func(name string, arg ...string) ([]byte, error) {
		if name == "fusermount3" {
			unmounted = append(unmounted, arg[1])
		}
		return nil, nil
	}

	var buf bytes.Buffer
	err := doUnmountAll(deps, &buf)

	if err != nil {
		t.Fatalf("doUnmountAll failed: %v", err)
	}

	if len(unmounted) != 2 {
		t.Fatalf("Expected 2 unmounts, got %d", len(unmounted))
	}

	if unmounted[0] != "/mnt/fake1" || unmounted[1] != "/mnt/fake2" {
		t.Errorf("Unexpected unmount targets: %v", unmounted)
	}

	outStr := buf.String()
	if !strings.Contains(outStr, "/mnt/fake1") || !strings.Contains(outStr, "/mnt/fake2") {
		t.Errorf("Expected output to report both mounts, got: %s", outStr)
	}
}
