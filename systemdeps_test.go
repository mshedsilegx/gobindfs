package main

import (
	"os"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// mockSystemDeps returns a SystemDeps instance with no-op or controlled mock functions.
func mockSystemDeps() *SystemDeps {
	return &SystemDeps{
		ReadFile: func(filename string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		ExecCombinedOutput: func(name string, arg ...string) ([]byte, error) {
			return nil, nil
		},
		SyscallUnmount: func(target string, flags int) error {
			return nil
		},
		FuseMount: func(mountpoint string, root fs.InodeEmbedder, options *fs.Options) (*fuse.Server, error) {
			return &fuse.Server{}, nil
		},
	}
}
