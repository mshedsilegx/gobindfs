package main

import (
	"context"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

var overrideNodePool = sync.Pool{
	New: func() interface{} {
		return &OverrideNode{}
	},
}

// OverrideNode is a custom go-fuse node that wraps the standard LoopbackNode.
// It intercepts FUSE operations to explicitly override the UID, GID, and file/directory
// permission modes to values specified by the user, ignoring the underlying file system's metadata.
type OverrideNode struct {
	*fs.LoopbackNode

	uid   uint32
	gid   uint32
	dmode uint32
	fmode uint32

	// Flags to track if we should actually override these values
	hasUid   bool
	hasGid   bool
	hasDmode bool
	hasFmode bool
}

// NewOverrideNode gets an OverrideNode from the pool and initializes it.
func NewOverrideNode(node *fs.LoopbackNode, n *OverrideNode) *OverrideNode {
	on := overrideNodePool.Get().(*OverrideNode)
	on.LoopbackNode = node
	on.uid = n.uid
	on.gid = n.gid
	on.dmode = n.dmode
	on.fmode = n.fmode
	on.hasUid = n.hasUid
	on.hasGid = n.hasGid
	on.hasDmode = n.hasDmode
	on.hasFmode = n.hasFmode
	return on
}

// Ensure OverrideNode implements NodeWrapChilder to recursively propagate itself
var _ fs.NodeWrapChilder = (*OverrideNode)(nil)

// Ensure OverrideNode implements necessary attr interfaces
var _ fs.NodeGetattrer = (*OverrideNode)(nil)
var _ fs.NodeSetattrer = (*OverrideNode)(nil)
var _ fs.NodeLookuper = (*OverrideNode)(nil)
var _ fs.NodeReaddirer = (*OverrideNode)(nil)

// WrapChild is called by go-fuse whenever a new inode is discovered.
// This allows us to recursively wrap all children with our OverrideNode.
func (n *OverrideNode) WrapChild(ctx context.Context, ops fs.InodeEmbedder) fs.InodeEmbedder {
	return NewOverrideNode(ops.(*fs.LoopbackNode), n)
}

// overrideAttr applies our custom UID, GID, and permissions to a FUSE Attr struct.
func (n *OverrideNode) overrideAttr(attr *fuse.Attr) {
	if n.hasUid {
		attr.Uid = n.uid
	}
	if n.hasGid {
		attr.Gid = n.gid
	}

	// Preserve the file type (directory, symlink, etc) but override permissions
	fileType := attr.Mode & syscall.S_IFMT
	if fileType == syscall.S_IFDIR && n.hasDmode {
		attr.Mode = fileType | (n.dmode & 07777)
	} else if fileType != syscall.S_IFDIR && n.hasFmode {
		attr.Mode = fileType | (n.fmode & 07777)
	}
}

// Getattr intercepts stat() calls to modify the returned metadata.
func (n *OverrideNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	status := n.LoopbackNode.Getattr(ctx, fh, out)
	if status == 0 {
		n.overrideAttr(&out.Attr)
	}
	return status
}

// Lookup intercepts directory lookups to modify the cached metadata returned to the kernel.
func (n *OverrideNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	node, status := n.LoopbackNode.Lookup(ctx, name, out)
	if status == 0 {
		n.overrideAttr(&out.Attr)
	}
	return node, status
}

// Setattr intercepts chmod/chown operations to prevent them from modifying the virtual mount or
// cascading down to the underlying real filesystem if the user is trying to change our overridden metadata.
func (n *OverrideNode) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	// Block operations that try to change mode, uid, or gid if we are overriding them
	if (in.Valid&fuse.FATTR_MODE != 0 && (n.hasFmode || n.hasDmode)) ||
		(in.Valid&fuse.FATTR_UID != 0 && n.hasUid) ||
		(in.Valid&fuse.FATTR_GID != 0 && n.hasGid) {
		return syscall.EPERM
	}

	// Pass through other legitimate Setattr operations like truncate, utimes, etc.
	status := n.LoopbackNode.Setattr(ctx, fh, in, out)
	if status == 0 {
		n.overrideAttr(&out.Attr)
	}
	return status
}

// Readdir intercepts readdir to modify the attributes returned in the directory stream.
func (n *OverrideNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	ds, errno := n.LoopbackNode.Readdir(ctx)
	if errno != 0 {
		return nil, errno
	}
	return &overrideDirStream{
		DirStream: ds,
		node:      n,
	}, 0
}

type overrideDirStream struct {
	fs.DirStream
	node *OverrideNode
}

func (ds *overrideDirStream) HasNext() bool {
	return ds.DirStream.HasNext()
}

func (ds *overrideDirStream) Next() (fuse.DirEntry, syscall.Errno) {
	entry, errno := ds.DirStream.Next()
	if errno == 0 {
		// DirEntry only has Mode, not Uid/Gid.
		// We override the permission bits in Mode.
		fileType := entry.Mode & syscall.S_IFMT
		if fileType == syscall.S_IFDIR && ds.node.hasDmode {
			entry.Mode = fileType | (ds.node.dmode & 07777)
		} else if fileType != syscall.S_IFDIR && ds.node.hasFmode {
			entry.Mode = fileType | (ds.node.fmode & 07777)
		}
	}
	return entry, errno
}
