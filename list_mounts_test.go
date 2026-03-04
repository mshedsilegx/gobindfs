package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestGetActiveMounts(t *testing.T) {
	deps := mockSystemDeps()
	mockProcMounts := `sysfs /sys sysfs rw,nosuid,nodev,noexec,relatime 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
/dev/loop0 /mnt/escaped\040path fuse.gobindfs rw,nosuid,nodev,relatime,user_id=1000,group_id=1000 0 0
/dev/loop1 /mnt/dmode fuse.gobindfs rw,nosuid,nodev,relatime,umask=0137,user_id=1000 0 0
`
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte(mockProcMounts), nil
		}
		return nil, os.ErrNotExist
	}

	mounts, err := getActiveMounts(deps)
	if err != nil {
		t.Fatalf("getActiveMounts failed: %v", err)
	}

	if len(mounts) != 2 {
		t.Fatalf("Expected 2 active mounts, got %d", len(mounts))
	}

	if mounts[0].Mountpoint != "/mnt/escaped path" {
		t.Errorf("Expected path /mnt/escaped path, got %q", mounts[0].Mountpoint)
	}
	if mounts[0].Original != "/dev/loop0" {
		t.Errorf("Expected orig /dev/loop0, got %q", mounts[0].Original)
	}

	if mounts[1].Mountpoint != "/mnt/dmode" {
		t.Errorf("Expected path /mnt/dmode, got %q", mounts[1].Mountpoint)
	}
	if mounts[1].Original != "/dev/loop1" {
		t.Errorf("Expected orig /dev/loop1, got %q", mounts[1].Original)
	}
}

func TestHandleListMounts(t *testing.T) {
	ResetMountsCache()
	deps := mockSystemDeps()
	mockProcMounts := `/dev/loop0 /mnt/active fuse.gobindfs rw 0 0`

	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte(mockProcMounts), nil
		}
		if name == "config.json" {
			return []byte(`[{"bindfs_mount": "/var/foo", "source_object": "/mnt/active", "uid": 1000}, {"bindfs_mount": "/var/bar", "source_object": "/mnt/missing"}]`), nil
		}
		return nil, os.ErrNotExist
	}

	cfg := &config{
		listMounts: true,
		multiMount: "config.json",
	}

	var buf bytes.Buffer
	err := handleListMounts(cfg, deps, &buf)

	if err == nil {
		t.Fatal("Expected error due to discrepancy (missing mount)")
	}
	if !strings.Contains(err.Error(), "discrepancies found") {
		t.Errorf("Expected discrepancy error, got: %v", err)
	}

	outStr := buf.String()
	if !strings.Contains(outStr, "/mnt/active") || !strings.Contains(outStr, "uid=1000") {
		t.Errorf("Expected output to contain /mnt/active and uid=1000, got: \n%s", outStr)
	}
	if !strings.Contains(outStr, "/mnt/missing") {
		t.Errorf("Expected output to contain /mnt/missing, got: \n%s", outStr)
	}
}

func TestHandleListMounts_PolicyReporting(t *testing.T) {
	ResetMountsCache()
	deps := mockSystemDeps()

	// Mock /proc/mounts with two entries:
	// 1. One with policy encoded in FsName (semicolon)
	// 2. One without policy (standard FUSE options only)
	mockProcMounts := `/usr/src/redhat/SOURCES;uid=1000,gid=100 /tmp/mnt/tools fuse.gobindfs rw,nosuid,nodev,relatime,user_id=0,group_id=0 0 0
/usr/src/redhat/SPECS /tmp/mnt/SPECS fuse.gobindfs rw,nosuid,nodev,relatime,user_id=1000,group_id=100 0 0
`
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte(mockProcMounts), nil
		}
		return nil, os.ErrNotExist
	}

	cfg := &config{
		listMounts: true,
	}

	var buf bytes.Buffer
	err := handleListMounts(cfg, deps, &buf)
	if err != nil {
		t.Fatalf("handleListMounts failed: %v", err)
	}

	outStr := buf.String()
	// Check first mount: should have the encoded policy
	if !strings.Contains(outStr, "/tmp/mnt/tools") || !strings.Contains(outStr, "uid=1000,gid=100") {
		t.Errorf("Expected /tmp/mnt/tools to have policy uid=1000,gid=100, got output:\n%s", outStr)
	}

	// Check second mount: should NOT have uid=1000,gid=100 because they come from standard FUSE user_id/group_id
	// which are implementation details of FUSE, not our metadata overrides.
	lines := strings.Split(outStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "/tmp/mnt/SPECS") {
			if strings.Contains(line, "uid=1000") || strings.Contains(line, "gid=100") {
				t.Errorf("Bug reproduced: /tmp/mnt/SPECS falsely reports policy from FUSE options: %q", line)
			}
		}
	}
}

func TestHandleListMountsEdgeCases(t *testing.T) {
	ResetMountsCache()
	deps := mockSystemDeps()
	var buf bytes.Buffer

	// Test config read error
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte{}, nil
		}
		return nil, os.ErrNotExist
	}
	cfg := &config{listMounts: true, multiMount: "missing.json"}
	err := handleListMounts(cfg, deps, &buf)
	if err == nil || !strings.Contains(err.Error(), "failed to read multiMount config") {
		t.Errorf("Expected config read error, got %v", err)
	}

	// Test invalid JSON config
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte{}, nil
		}
		if name == "invalid.json" {
			return []byte(`[{"bindfs_mount": "/tmp", "source_object": "/tmp", ]`), nil
		}
		return nil, os.ErrNotExist
	}
	cfg = &config{listMounts: true, multiMount: "invalid.json"}
	err = handleListMounts(cfg, deps, &buf)
	if err == nil || !strings.Contains(err.Error(), "failed to parse multiMount config") {
		t.Errorf("Expected parse error, got %v", err)
	}
}
