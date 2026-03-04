package main

import (
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// SystemDeps provides abstractions for OS-level and external dependencies,
// enabling robust unit testing without relying on the physical host system.
type SystemDeps struct {
	ReadFile           func(filename string) ([]byte, error)
	ExecCombinedOutput func(name string, arg ...string) ([]byte, error)
	SyscallUnmount     func(target string, flags int) error
	FuseMount          func(mountpoint string, root fs.InodeEmbedder, options *fs.Options) (*fuse.Server, error)
	Stdout             io.Writer
}

// defaultSystemDeps returns a SystemDeps instance wired to the real OS functions.
func defaultSystemDeps() *SystemDeps {
	return &SystemDeps{
		ReadFile: os.ReadFile,
		// #nosec G204 G702
		ExecCombinedOutput: func(name string, arg ...string) ([]byte, error) {
			return exec.Command(name, arg...).CombinedOutput()
		},
		SyscallUnmount: syscall.Unmount,
		FuseMount:      fs.Mount,
		Stdout:         os.Stdout,
	}
}
