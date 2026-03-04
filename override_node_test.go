package main

import (
	"context"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestOverrideNode_MetadataOverrides(t *testing.T) {
	// 1. Setup a base loopback node
	tmpDir := t.TempDir()
	baseRoot, err := fs.NewLoopbackRoot(tmpDir)
	if err != nil {
		t.Fatalf("NewLoopbackRoot failed: %v", err)
	}

	// 2. Create OverrideNode with specific overrides
	expectedUid := uint32(1001)
	expectedGid := uint32(1002)
	expectedFmode := uint32(0600)
	expectedDmode := uint32(0700)

	node := &OverrideNode{
		LoopbackNode: baseRoot.(*fs.LoopbackNode),
		uid:          expectedUid,
		gid:          expectedGid,
		fmode:        expectedFmode,
		dmode:        expectedDmode,
		hasUid:       true,
		hasGid:       true,
		hasFmode:     true,
		hasDmode:     true,
	}

	// 3. Test overrideAttr for a file
	t.Run("override_file_attr", func(t *testing.T) {
		attr := &fuse.Attr{
			Mode: syscall.S_IFREG | 0644,
		}
		attr.Uid = 0
		attr.Gid = 0
		node.overrideAttr(attr)

		if attr.Uid != expectedUid {
			t.Errorf("Expected Uid %d, got %d", expectedUid, attr.Uid)
		}
		if attr.Gid != expectedGid {
			t.Errorf("Expected Gid %d, got %d", expectedGid, attr.Gid)
		}
		if attr.Mode != (syscall.S_IFREG | expectedFmode) {
			t.Errorf("Expected Mode %o, got %o", syscall.S_IFREG|expectedFmode, attr.Mode)
		}
	})

	// 4. Test overrideAttr for a directory
	t.Run("override_directory_attr", func(t *testing.T) {
		attr := &fuse.Attr{
			Mode: syscall.S_IFDIR | 0755,
		}
		attr.Uid = 0
		attr.Gid = 0
		node.overrideAttr(attr)

		if attr.Uid != expectedUid {
			t.Errorf("Expected Uid %d, got %d", expectedUid, attr.Uid)
		}
		if attr.Gid != expectedGid {
			t.Errorf("Expected Gid %d, got %d", expectedGid, attr.Gid)
		}
		if attr.Mode != (syscall.S_IFDIR | expectedDmode) {
			t.Errorf("Expected Mode %o, got %o", syscall.S_IFDIR|expectedDmode, attr.Mode)
		}
	})

	// 5. Test Setattr blocking
	t.Run("block_restricted_setattr", func(t *testing.T) {
		ctx := context.Background()
		in := &fuse.SetAttrIn{}
		in.Valid = fuse.FATTR_MODE
		in.Mode = 0777
		out := &fuse.AttrOut{}

		status := node.Setattr(ctx, nil, in, out)
		if status != syscall.EPERM {
			t.Errorf("Expected EPERM when overriding mode, got %v", status)
		}

		in.Valid = fuse.FATTR_UID
		status = node.Setattr(ctx, nil, in, out)
		if status != syscall.EPERM {
			t.Errorf("Expected EPERM when overriding UID, got %v", status)
		}

		in.Valid = fuse.FATTR_GID
		status = node.Setattr(ctx, nil, in, out)
		if status != syscall.EPERM {
			t.Errorf("Expected EPERM when overriding GID, got %v", status)
		}
	})
}

func TestOverrideNode_WrapChildInitialization(t *testing.T) {
	tmpDir := t.TempDir()
	baseRoot, _ := fs.NewLoopbackRoot(tmpDir)

	parent := &OverrideNode{
		LoopbackNode: baseRoot.(*fs.LoopbackNode),
		uid:          1000,
		hasUid:       true,
	}

	childOps := &fs.LoopbackNode{}
	wrappedChild := parent.WrapChild(context.Background(), childOps)

	childNode, ok := wrappedChild.(*OverrideNode)
	if !ok {
		t.Fatal("WrapChild did not return an *OverrideNode")
	}

	if childNode.LoopbackNode != childOps {
		t.Error("Child LoopbackNode not correctly initialized")
	}
	if childNode.uid != parent.uid || !childNode.hasUid {
		t.Error("Child metadata overrides not propagated")
	}
}
