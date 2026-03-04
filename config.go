package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
)

// MountConfig maps the JSON multi-mount structure into native Go types.
type MountConfig struct {
	BindfsMount  string   `json:"bindfs_mount"`
	SourceObject string   `json:"source_object"`
	Comment      string   `json:"comment"`
	Options      []string `json:"options"`
	Uid          *uint32  `json:"uid,omitempty"`
	Gid          *uint32  `json:"gid,omitempty"`
	Dmode        string   `json:"dmode,omitempty"`
	Fmode        string   `json:"fmode,omitempty"`
}

// config holds all the command-line options and arguments required
// to configure and mount the gobindfs filesystem.
type config struct {
	debug             bool
	allowOther        bool
	idMapped          bool
	quiet             bool
	ro                bool
	directMount       bool
	directMountStrict bool
	cpuProfile        string
	memProfile        string
	version           bool
	multiMount        string
	listMounts        bool
	umount            string
	umountAll         bool
	options           string
	detach            bool
	uid               int
	gid               int
	dmode             string
	fmode             string
	mountPoint        string
	sourceObject      string
}

// parseFlags parses command-line flags.
// It handles configuring flags for basic options, performance profiling,
// single/multi mounting modes, and unmounting sub-commands.
//
// After parsing, it attempts to clean and convert `-mount` and `-source`
// directory paths into absolute paths, evaluating any symlinks
// inside the file names to prevent recursive lookup loops during mount.
func parseFlags(args []string) (*config, error) {
	cfg := &config{}

	fs := flag.NewFlagSet("gobindfs", flag.ContinueOnError)

	fs.BoolVar(&cfg.debug, "debug", false, "print debugging messages.")
	fs.BoolVar(&cfg.allowOther, "allow-other", false, "mount with -o allowother.")
	fs.BoolVar(&cfg.idMapped, "idmapped", false, "enable id-mapped mount")
	fs.BoolVar(&cfg.quiet, "quiet", false, "quiet")
	fs.BoolVar(&cfg.ro, "ro", false, "mount read-only")
	fs.BoolVar(&cfg.directMount, "directmount", false, "try to call the mount syscall instead of executing fusermount")
	fs.BoolVar(&cfg.directMountStrict, "directmountstrict", false, "like directMount, but don't fall back to fusermount")
	fs.StringVar(&cfg.cpuProfile, "cpuprofile", "", "write cpu profile to this file")
	fs.StringVar(&cfg.memProfile, "memprofile", "", "write memory profile to this file")
	fs.BoolVar(&cfg.version, "version", false, "print version and exit")
	fs.StringVar(&cfg.multiMount, "multimount", "", "path to JSON config file for parallel multi-mounting")
	fs.BoolVar(&cfg.listMounts, "listmounts", false, "display a nicely formatted table of active gobindfs mounts")
	fs.StringVar(&cfg.umount, "umount", "", "safely unmount a specific mountpoint and exit")
	fs.BoolVar(&cfg.umountAll, "umount-all", false, "safely unmount all active gobindfs mounts and exit")
	fs.StringVar(&cfg.options, "o", "", "comma separated list of low level FUSE options (e.g. allow_other,ro,max_read=131072)")
	fs.BoolVar(&cfg.detach, "detach", false, "put the process in the background and return to prompt")
	fs.StringVar(&cfg.mountPoint, "mount", "", "mountpoint directory path")
	fs.StringVar(&cfg.sourceObject, "source", "", "source object directory path")
	fs.IntVar(&cfg.uid, "uid", -1, "override file and directory UID")
	fs.IntVar(&cfg.gid, "gid", -1, "override file and directory GID")
	fs.StringVar(&cfg.dmode, "dmode", "", "override directory permissions (e.g. 755)")
	fs.StringVar(&cfg.fmode, "fmode", "", "override file permissions (e.g. 644)")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "usage: %s [options] -mount <mountpoint> -source <source_object>\n", "gobindfs")
		_, _ = fmt.Fprintf(fs.Output(), "\noptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil, err
		}
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	if fs.NArg() > 0 {
		return nil, fmt.Errorf("positional arguments are no longer supported. Please use -mount and -source flags instead")
	}

	// 1. Validate mutual exclusivity of standalone sub-commands
	subCmdCount := 0
	if cfg.version {
		subCmdCount++
	}
	if cfg.listMounts {
		subCmdCount++
	}
	if cfg.umount != "" {
		subCmdCount++
	}
	if cfg.umountAll {
		subCmdCount++
	}
	if subCmdCount > 1 {
		return nil, fmt.Errorf("flags -version, -listMounts, -umount, and -umount-all are standalone commands and cannot be used together")
	}

	// 2. Validate that single-mount specific flags are not mixed with multiMount
	if cfg.multiMount != "" {
		if cfg.uid != -1 || cfg.gid != -1 || cfg.dmode != "" || cfg.fmode != "" || cfg.options != "" || cfg.ro || cfg.mountPoint != "" || cfg.sourceObject != "" {
			return nil, fmt.Errorf("flags -uid, -gid, -dmode, -fmode, -o, -ro, -mount, and -source cannot be used with -multimount")
		}
	}

	// 3. Validate that mount configuration flags are not used with standalone commands
	// Note: listMounts DOES accept multiMount, to reconcile configs. So we exclude it from this check.
	isStandalone := cfg.version || cfg.umount != "" || cfg.umountAll
	if isStandalone {
		if cfg.uid != -1 || cfg.gid != -1 || cfg.dmode != "" || cfg.fmode != "" || cfg.options != "" || cfg.ro || cfg.multiMount != "" || cfg.allowOther || cfg.idMapped || cfg.directMount || cfg.directMountStrict || cfg.mountPoint != "" || cfg.sourceObject != "" {
			return nil, fmt.Errorf("mount configuration flags cannot be used alongside standalone commands (-version, -umount, -umount-all)")
		}
		return cfg, nil
	}

	if cfg.listMounts {
		if cfg.uid != -1 || cfg.gid != -1 || cfg.dmode != "" || cfg.fmode != "" || cfg.options != "" || cfg.ro || cfg.allowOther || cfg.idMapped || cfg.directMount || cfg.directMountStrict || cfg.mountPoint != "" || cfg.sourceObject != "" {
			return nil, fmt.Errorf("mount configuration flags cannot be used alongside -listmounts")
		}
		return cfg, nil
	}

	if cfg.cpuProfile != "" {
		cfg.cpuProfile = filepath.Clean(cfg.cpuProfile)
	}
	if cfg.memProfile != "" {
		cfg.memProfile = filepath.Clean(cfg.memProfile)
	}

	if cfg.umount != "" || cfg.umountAll {
		// No mount directories required for unmount operations.
		return cfg, nil
	}

	if cfg.multiMount != "" {
		// For multimount, we don't validate mountpoint and source here.
		// They will be loaded from JSON and validated inside setupMultiMount.
		cfg.multiMount = filepath.Clean(cfg.multiMount)
		return cfg, nil
	}

	if cfg.mountPoint == "" || cfg.sourceObject == "" {
		return nil, fmt.Errorf("missing -mount and -source arguments")
	}

	cfg.mountPoint = filepath.Clean(cfg.mountPoint)
	cfg.sourceObject = filepath.Clean(cfg.sourceObject)

	// Attempt to evaluate symlinks. This is critical for robust matching and safety.
	// If the file doesn't exist yet, EvalSymlinks fails, so we just use the Cleaned path.
	// This ensures we get the real path if it exists, or the best possible string representation if it doesn't.
	// We do NOT fail here if they don't exist; we just pass the cleaned path forward, and
	// we will evaluate it during runMount to ensure symlinks aren't blocking setup.
	if evaluatedMp, err := filepath.EvalSymlinks(cfg.mountPoint); err == nil {
		cfg.mountPoint = evaluatedMp
	} else if !cfg.quiet {
		// #nosec G706 -- user input is sanitized before logging
		safeMp := filepath.Clean(cfg.mountPoint)
		log.Printf("Warning: Failed to evaluate symlinks for mountpoint %q: %v. Proceeding with cleaned path.", safeMp, err)
	}

	if evaluatedSrc, err := filepath.EvalSymlinks(cfg.sourceObject); err == nil {
		cfg.sourceObject = evaluatedSrc
	} else if !cfg.quiet {
		// #nosec G706 -- user input is sanitized before logging
		safeSrc := filepath.Clean(cfg.sourceObject)
		log.Printf("Warning: Failed to evaluate symlinks for source object %q: %v. Proceeding with cleaned path.", safeSrc, err)
	}

	return cfg, nil
}
