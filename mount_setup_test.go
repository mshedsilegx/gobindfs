package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestSetupMultiMountEdgeCases(t *testing.T) {
	deps := mockSystemDeps()
	var buf bytes.Buffer

	// Test missing bindfs_mount
	cfg := &config{multiMount: "bad.json"}
	deps.ReadFile = func(name string) ([]byte, error) {
		// Use a path that is guaranteed not to exist even after Cleaning/Absoluting
		return []byte(`[{"source_object": "/tmp/source/exists"}]`), nil
	}
	// We need to mock os.Open to return an error for the empty bindfs_mount (which becomes ".")
	// Since setupMultiMount uses os.Open, we can't easily mock it without refactoring to use SystemDeps.
	// However, in the current implementation of setupMultiMount, if bindfs_mount is missing,
	// it defaults to "." (current directory), which usually exists.
	// Let's modify the test to provide an explicit non-existent path.
	deps.ReadFile = func(name string) ([]byte, error) {
		return []byte(`[{"bindfs_mount": "/non/existent/mount", "source_object": "/tmp/source"}]`), nil
	}
	_, err := setupMultiMount(cfg, deps, &buf)
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("Expected bindfs missing failure (directory), got %v", err)
	}

	// Test missing bindfs_mount correctly
	deps.ReadFile = func(name string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"bindfs_mount": "/does/not/exist/literally", "source_object": "%s"}]`, t.TempDir())), nil
	}
	_, err = setupMultiMount(cfg, deps, &buf)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error for missing bindfs_mount, got %v", err)
	}

	// Test missing source_object
	deps.ReadFile = func(name string) ([]byte, error) {
		return []byte(fmt.Sprintf(`[{"bindfs_mount": "%s", "source_object": "/does/not/exist/literally"}]`, t.TempDir())), nil
	}
	_, err = setupMultiMount(cfg, deps, &buf)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error for missing source_object, got %v", err)
	}

	// Test invalid JSON
	deps.ReadFile = func(name string) ([]byte, error) {
		return []byte(`[{"bindfs_mount": "/tmp", "source_object": "/tmp", ]`), nil // invalid syntax
	}
	_, err = setupMultiMount(cfg, deps, &buf)
	if err == nil || !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("Expected parse error, got %v", err)
	}
}

func TestRunMountEdgeCases(t *testing.T) {
	deps := mockSystemDeps()
	cfg := &config{}

	// Test missing source object
	_, err := runMount(cfg, "/does/not/exist", t.TempDir(), nil, deps)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error for missing source object, got %v", err)
	}

	// Test missing mount point
	_, err = runMount(cfg, t.TempDir(), "/does/not/exist", nil, deps)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected error for missing mount point, got %v", err)
	}

	// Test source object is not a directory
	tmpFile, _ := os.CreateTemp("", "file*")
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	_, err = runMount(cfg, tmpFile.Name(), t.TempDir(), nil, deps)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Expected error for non-directory source object, got %v", err)
	}

	// Test mount point is not a directory
	tmpFile2, _ := os.CreateTemp("", "file*")
	defer func() {
		_ = tmpFile2.Close()
		_ = os.Remove(tmpFile2.Name())
	}()

	_, err = runMount(cfg, t.TempDir(), tmpFile2.Name(), nil, deps)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Expected error for non-directory mount point, got %v", err)
	}
}

func TestSetupMultiMount_Success(t *testing.T) {
	deps := mockSystemDeps()
	mntDir := t.TempDir()
	srcDir := t.TempDir()
	jsonConfig := fmt.Sprintf(`[{"bindfs_mount": "%s", "source_object": "%s"}]`, mntDir, srcDir)

	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "good.json" {
			return []byte(jsonConfig), nil
		}
		if name == "/proc/mounts" {
			return []byte(""), nil // No existing mounts
		}
		return nil, os.ErrNotExist
	}

	var capturedMountpoint string
	var capturedSourceObject string
	deps.FuseMount = func(mountpoint string, root fs.InodeEmbedder, options *fs.Options) (*fuse.Server, error) {
		capturedMountpoint = mountpoint
		capturedSourceObject = options.FsName
		return &fuse.Server{}, nil
	}

	cfg := &config{
		multiMount: "good.json",
		quiet:      true,
	}

	var buf bytes.Buffer
	servers, err := setupMultiMount(cfg, deps, &buf)
	if err != nil {
		t.Fatalf("Expected success, got %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}

	// Verify that arguments were not reversed
	if capturedMountpoint != mntDir {
		t.Errorf("Expected mountpoint %q, got %q", mntDir, capturedMountpoint)
	}
	// The source object might have encoded policy suffix
	if !strings.HasPrefix(capturedSourceObject, srcDir) {
		t.Errorf("Expected source object prefix %q, got %q", srcDir, capturedSourceObject)
	}
}

func TestSetupMultiMount_InvalidPath(t *testing.T) {
	deps := mockSystemDeps()
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "bad.json" {
			return []byte(`[{"bindfs_mount": "/does/not/exist/ever", "source_object": "/tmp"}]`), nil
		}
		return nil, os.ErrNotExist
	}

	cfg := &config{
		multiMount: "bad.json",
	}

	var buf bytes.Buffer
	servers, err := setupMultiMount(cfg, deps, &buf)
	if err == nil {
		t.Fatalf("Expected error for non-existent chroot directory")
	}
	if servers != nil {
		t.Errorf("Expected nil servers on failure")
	}
}

func TestRunSingleMount(t *testing.T) {
	deps := mockSystemDeps()
	mntDir := t.TempDir()
	srcDir := t.TempDir()
	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "/proc/mounts" {
			return []byte(""), nil // No existing mounts
		}
		return nil, os.ErrNotExist
	}

	cfg := &config{
		mountPoint:   mntDir,
		sourceObject: srcDir,
		quiet:        true,
		dmode:        "640", // test dmode calculation
	}

	var capturedMountpoint string
	var capturedSourceObject string
	deps.FuseMount = func(mountpoint string, root fs.InodeEmbedder, options *fs.Options) (*fuse.Server, error) {
		capturedMountpoint = mountpoint
		capturedSourceObject = options.FsName
		return &fuse.Server{}, nil
	}

	var buf bytes.Buffer
	servers, err := setupSingleMount(cfg, deps, &buf)
	if err != nil {
		t.Fatalf("Expected success, got %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}

	// Verify that arguments were not reversed
	if capturedMountpoint != mntDir {
		t.Errorf("Expected mountpoint %q, got %q", mntDir, capturedMountpoint)
	}
	// The source object might have encoded policy suffix
	if !strings.HasPrefix(capturedSourceObject, srcDir) {
		t.Errorf("Expected source object prefix %q, got %q", srcDir, capturedSourceObject)
	}
}

func TestSetupMultiMount_OptionsParsing(t *testing.T) {
	deps := mockSystemDeps()
	mntDir := t.TempDir()
	srcDir := t.TempDir()

	// Test both formats: "key=val" and "key val"
	jsonConfig := fmt.Sprintf(`[
  {
    "bindfs_mount": "%s", 
    "source_object": "%s",
    "options": ["fmode=600", "dmode 700", "uid 1000", "gid=100", "allow_other"]
  }
]`, mntDir, srcDir)

	deps.ReadFile = func(name string) ([]byte, error) {
		if name == "options.json" {
			return []byte(jsonConfig), nil
		}
		if name == "/proc/mounts" {
			return []byte(""), nil
		}
		return nil, os.ErrNotExist
	}

	var capturedFsName string
	var capturedOptions []string
	deps.FuseMount = func(mountpoint string, root fs.InodeEmbedder, options *fs.Options) (*fuse.Server, error) {
		capturedFsName = options.FsName
		capturedOptions = options.Options
		return &fuse.Server{}, nil
	}

	cfg := &config{multiMount: "options.json", quiet: true}
	var buf bytes.Buffer
	_, err := setupMultiMount(cfg, deps, &buf)
	if err != nil {
		t.Fatalf("Expected success, got %v", err)
	}

	// Verify policy encoding in FsName
	// Note: The order of flags in the encoded string depends on the order in the options array
	if !strings.Contains(capturedFsName, "fmode=600") {
		t.Errorf("Expected FsName to contain fmode=600, got %q", capturedFsName)
	}
	if !strings.Contains(capturedFsName, "dmode=700") {
		t.Errorf("Expected FsName to contain dmode=700, got %q", capturedFsName)
	}
	if !strings.Contains(capturedFsName, "uid=1000") {
		t.Errorf("Expected FsName to contain uid=1000, got %q", capturedFsName)
	}
	if !strings.Contains(capturedFsName, "gid=100") {
		t.Errorf("Expected FsName to contain gid=100, got %q", capturedFsName)
	}

	// Verify metadata flags were REMOVED from Options passed to FUSE
	for _, opt := range capturedOptions {
		if strings.HasPrefix(opt, "fmode") || strings.HasPrefix(opt, "dmode") ||
			strings.HasPrefix(opt, "uid") || strings.HasPrefix(opt, "gid") {
			t.Errorf("Metadata option %q should have been filtered out of FUSE options", opt)
		}
	}

	// Verify standard option was KEPT
	foundAllowOther := false
	for _, opt := range capturedOptions {
		if opt == "allow_other" {
			foundAllowOther = true
			break
		}
	}
	if !foundAllowOther {
		t.Errorf("Standard option 'allow_other' should have been preserved")
	}
}
