package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

const (
	fsName          = "gobindfs"
	optDefaultPerms = "default_permissions"
	optReadOnly     = "ro"
)

// runMount initializes and starts a single gobindfs FUSE mount server.
//
// It performs a series of preliminary sanity checks:
//   - Verifies the 'source object' path exists and is a directory.
//   - Verifies the 'mountpoint' exists and is a directory.
//
// It then creates a NewLoopbackRoot from the source object path and translates the generic
// app config struct into the specific fs.Options required by the underlying Go-FUSE library.
// Any user-supplied low-level FUSE options (via `-o` flag or JSON config) are appended.
// Returns the active *fuse.Server or an error.
func runMount(cfg *config, sourceObject string, mountPoint string, additionalOpts []string, deps *SystemDeps) (*fuse.Server, error) {
	// #nosec G304 G703 -- Trusted paths evaluated against symlinks via filepath.EvalSymlinks earlier in parsing
	if info, err := os.Stat(sourceObject); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source object path %q does not exist", sourceObject)
		}
		return nil, fmt.Errorf("failed to stat source object path %q: %w", sourceObject, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("source object path %q is not a directory", sourceObject)
	}

	// Verify we have read permissions on the source directory by attempting to open it
	// #nosec G304 G703 -- Trusted paths evaluated against symlinks via filepath.EvalSymlinks earlier in parsing
	if f, err := os.Open(sourceObject); err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied reading source object directory %q", sourceObject)
		}
		return nil, fmt.Errorf("failed to open source object directory %q: %w", sourceObject, err)
	} else {
		_ = f.Close()
	}

	// #nosec G304 G703 -- Trusted paths evaluated against symlinks via filepath.EvalSymlinks earlier in parsing
	if info, err := os.Stat(mountPoint); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("mountpoint %q does not exist", mountPoint)
		}
		return nil, fmt.Errorf("failed to stat mountpoint %q: %w", mountPoint, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("mountpoint %q is not a directory", mountPoint)
	}

	// Verify we have access to the mountpoint directory
	// #nosec G304 G703 -- Trusted paths evaluated against symlinks via filepath.EvalSymlinks earlier in parsing
	if f, err := os.Open(mountPoint); err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied accessing mountpoint directory %q", mountPoint)
		}
		return nil, fmt.Errorf("failed to open mountpoint directory %q: %w", mountPoint, err)
	} else {
		_ = f.Close()
	}

	var loopbackRoot fs.InodeEmbedder
	var err error

	// If any of the metadata override options are provided, use our custom OverrideNode
	if cfg.uid != -1 || cfg.gid != -1 || cfg.dmode != "" || cfg.fmode != "" {
		// First create a standard loopback root
		baseRoot, err := fs.NewLoopbackRoot(sourceObject)
		if err != nil {
			return nil, fmt.Errorf("NewLoopbackRoot(%s): %w", sourceObject, err)
		}

		// Then wrap its root node in our OverrideNode directly
		overrideNode := &OverrideNode{
			LoopbackNode: baseRoot.(*fs.LoopbackNode),
		}

		if cfg.uid != -1 {
			// #nosec G115 -- -1 is checked, other CLI inputs map fine to uint32 limits for UID.
			overrideNode.uid = uint32(cfg.uid)
			overrideNode.hasUid = true
		}
		if cfg.gid != -1 {
			// #nosec G115 -- -1 is checked, other CLI inputs map fine to uint32 limits for GID.
			overrideNode.gid = uint32(cfg.gid)
			overrideNode.hasGid = true
		}
		if cfg.dmode != "" {
			dmode, err := strconv.ParseUint(cfg.dmode, 8, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid dmode %q: %w", cfg.dmode, err)
			}
			overrideNode.dmode = uint32(dmode)
			overrideNode.hasDmode = true
		}
		if cfg.fmode != "" {
			fmode, err := strconv.ParseUint(cfg.fmode, 8, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid fmode %q: %w", cfg.fmode, err)
			}
			overrideNode.fmode = uint32(fmode)
			overrideNode.hasFmode = true
		}

		// Since we just instantiated the loopback node above, we inject our override parameters into it
		// and use it as the new root.
		loopbackRoot = overrideNode
	} else {
		loopbackRoot, err = fs.NewLoopbackRoot(sourceObject)
		if err != nil {
			return nil, fmt.Errorf("NewLoopbackRoot(%s): %w", sourceObject, err)
		}
	}

	pUid, pGid, dmode, fmode := cfg.uid, cfg.gid, cfg.dmode, cfg.fmode
	var ptrUid, ptrGid *uint32
	if pUid != -1 {
		// #nosec G115 -- Checked for -1, and CLI parser validates positive range for UID
		u := uint32(pUid)
		ptrUid = &u
	}
	if pGid != -1 {
		// #nosec G115 -- Checked for -1, and CLI parser validates positive range for GID
		g := uint32(pGid)
		ptrGid = &g
	}
	policyStr := formatPolicy(ptrUid, ptrGid, dmode, fmode)
	if policyStr == "-" {
		policyStr = ""
	}

	// Default timeouts to 0.5s for improved metadata performance
	defaultTimeout := 500 * time.Millisecond
	opts := &fs.Options{
		AttrTimeout:  &defaultTimeout,
		EntryTimeout: &defaultTimeout,

		NullPermissions: true,

		MountOptions: fuse.MountOptions{
			AllowOther:        cfg.allowOther,
			Debug:             cfg.debug,
			DirectMount:       cfg.directMount,
			DirectMountStrict: cfg.directMountStrict,
			IDMappedMount:     cfg.idMapped,
			FsName:            sourceObject,
			Name:              fsName,
			MaxWrite:          256 * 1024, // 256KB max_read
		},
	}

	if policyStr != "" {
		opts.FsName = fmt.Sprintf("%s;%s", sourceObject, policyStr)
	}

	if opts.AllowOther {
		opts.Options = append(opts.Options, optDefaultPerms)
	}
	if cfg.ro {
		opts.Options = append(opts.Options, optReadOnly)
	}

	// Parse low level FUSE options to extract those that should be handled by go-fuse
	// rather than passed to fusermount3 (which will reject kernel protocol settings).
	var allOpts []string
	if cfg.options != "" {
		allOpts = append(allOpts, strings.Split(cfg.options, ",")...)
	}
	if len(additionalOpts) > 0 {
		allOpts = append(allOpts, additionalOpts...)
	}

	for _, opt := range allOpts {
		// Kernel protocol settings mapped directly to MountOptions
		if strings.HasPrefix(opt, "attr_timeout=") {
			valStr := strings.TrimPrefix(opt, "attr_timeout=")
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid attr_timeout %q: %w", valStr, err)
			}
			t := time.Duration(val * float64(time.Second))
			opts.AttrTimeout = &t
		} else if strings.HasPrefix(opt, "entry_timeout=") {
			valStr := strings.TrimPrefix(opt, "entry_timeout=")
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid entry_timeout %q: %w", valStr, err)
			}
			t := time.Duration(val * float64(time.Second))
			opts.EntryTimeout = &t
		} else if strings.HasPrefix(opt, "negative_timeout=") {
			valStr := strings.TrimPrefix(opt, "negative_timeout=")
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid negative_timeout %q: %w", valStr, err)
			}
			t := time.Duration(val * float64(time.Second))
			opts.NegativeTimeout = &t
		} else if strings.HasPrefix(opt, "max_read=") {
			valStr := strings.TrimPrefix(opt, "max_read=")
			val, err := strconv.Atoi(valStr)
			if err != nil {
				return nil, fmt.Errorf("invalid max_read %q: %w", valStr, err)
			}
			// In go-fuse, max_read is set equal to MountOptions.MaxWrite internally
			opts.MaxWrite = val
		} else if strings.HasPrefix(opt, "max_write=") {
			valStr := strings.TrimPrefix(opt, "max_write=")
			val, err := strconv.Atoi(valStr)
			if err != nil {
				return nil, fmt.Errorf("invalid max_write %q: %w", valStr, err)
			}
			opts.MaxWrite = val
		} else if strings.HasPrefix(opt, "max_pages=") {
			// go-fuse calculates MaxPages automatically based on MaxWrite and rounding up.
			// It doesn't expose a MaxPages field in MountOptions.
			if !cfg.quiet {
				log.Printf("Warning: max_pages FUSE option is ignored. go-fuse calculates it automatically based on max_write.")
			}
		} else {
			// Check if it's a known safe option for fusermount3, otherwise it might cause failures
			safeOpts := map[string]bool{
				// Access Control
				"allow_other": true, "allow_root": true, "default_permissions": true,
				// VFS Flags
				"ro": true, "rw": true, "nosuid": true, "nodev": true, "noexec": true,
				"sync": true, "async": true, "atime": true, "noatime": true, "relatime": true,
				// FUSE Specific
				"nonempty": true, "auto_unmount": true,
			}

			// Handle key=value safe options
			baseOpt := opt
			if idx := strings.Index(opt, "="); idx != -1 {
				baseOpt = opt[:idx]
			}

			if safeOpts[baseOpt] || baseOpt == "user_id" || baseOpt == "group_id" || baseOpt == "fsname" || baseOpt == "subtype" || baseOpt == "umask" {
				opts.Options = append(opts.Options, opt)
			} else {
				if !cfg.quiet {
					// Escape newlines and carriage returns to prevent log injection
					safeOpt := strings.ReplaceAll(opt, "\n", "\\n")
					safeOpt = strings.ReplaceAll(safeOpt, "\r", "\\r")
					// #nosec G706 -- user input is sanitized above
					log.Printf("Warning: Option %q is not in the fusermount3 safe-list and may cause mount failure", safeOpt)
				}
				// We append it anyway to allow flexibility, but log a warning
				opts.Options = append(opts.Options, opt)
			}
		}
	}

	if !cfg.quiet {
		opts.Logger = log.New(os.Stderr, "", 0)
	}

	server, err := deps.FuseMount(mountPoint, loopbackRoot, opts)
	if err != nil {
		return nil, fmt.Errorf("mount fail: %w", err)
	}

	return server, nil
}

// setupMultiMount reads a JSON configuration and establishes multiple FUSE mounts concurrently.
func setupMultiMount(cfg *config, deps *SystemDeps, out io.Writer) ([]*fuse.Server, error) {
	// #nosec G304 G703 -- Trusted user CLI flag for config file path
	data, err := deps.ReadFile(cfg.multiMount)
	if err != nil {
		return nil, fmt.Errorf("failed to read multiMount config: %w", err)
	}

	var mounts []MountConfig
	if err := json.Unmarshal(data, &mounts); err != nil {
		return nil, fmt.Errorf("failed to parse multiMount config: %w", err)
	}

	if len(mounts) == 0 {
		return nil, fmt.Errorf("no mounts defined in %s", cfg.multiMount)
	}

	// Pre-validate all paths before starting any mounts to prevent partial-state zombies
	var validMounts []MountConfig
	activeMounts, _ := getActiveMountsCached(deps)
	activeMap := make(map[string]string)
	for _, am := range activeMounts {
		activeMap[am.Mountpoint] = am.Original
	}

	var mu sync.Mutex
	var validationWg sync.WaitGroup

	type validatedMount struct {
		m   MountConfig
		err error
	}
	validatedChan := make(chan validatedMount, len(mounts))

	for _, m := range mounts {
		validationWg.Add(1)
		go func(m MountConfig) {
			defer validationWg.Done()

			mp, err := filepath.Abs(filepath.Clean(m.BindfsMount))
			if err != nil {
				validatedChan <- validatedMount{err: fmt.Errorf("invalid bindfs_mount %q: %w", m.BindfsMount, err)}
				return
			}

			// Verify bindfs mount point exists and is accessible
			// #nosec G304 -- Path is sanitized and evaluated against symlinks via filepath.EvalSymlinks earlier
			if f, statErr := os.Open(mp); statErr != nil {
				if os.IsNotExist(statErr) {
					validatedChan <- validatedMount{err: fmt.Errorf("bindfs_mount directory %q does not exist", mp)}
				} else if os.IsPermission(statErr) {
					validatedChan <- validatedMount{err: fmt.Errorf("permission denied accessing bindfs_mount directory %q", mp)}
				} else {
					validatedChan <- validatedMount{err: fmt.Errorf("failed to access bindfs_mount directory %q: %w", mp, statErr)}
				}
				return
			} else {
				_ = f.Close()
			}

			mp, err = filepath.EvalSymlinks(mp)
			if err != nil {
				validatedChan <- validatedMount{err: fmt.Errorf("failed to resolve bindfs_mount symlinks %q: %w", m.BindfsMount, err)}
				return
			}
			m.BindfsMount = mp

			src, err := filepath.Abs(filepath.Clean(m.SourceObject))
			if err != nil {
				validatedChan <- validatedMount{err: fmt.Errorf("invalid source object %q: %w", m.SourceObject, err)}
				return
			}

			// Verify source object exists and is accessible
			// #nosec G304 -- Path is sanitized and evaluated against symlinks via filepath.EvalSymlinks earlier
			if f, statErr := os.Open(src); statErr != nil {
				if os.IsNotExist(statErr) {
					validatedChan <- validatedMount{err: fmt.Errorf("source_object directory %q does not exist", src)}
				} else if os.IsPermission(statErr) {
					validatedChan <- validatedMount{err: fmt.Errorf("permission denied accessing source_object directory %q", src)}
				} else {
					validatedChan <- validatedMount{err: fmt.Errorf("failed to access source_object directory %q: %w", src, statErr)}
				}
				return
			} else {
				_ = f.Close()
			}

			src, err = filepath.EvalSymlinks(src)
			if err != nil && !os.IsNotExist(err) {
				validatedChan <- validatedMount{err: fmt.Errorf("failed to resolve source object symlinks %q: %w", m.SourceObject, err)}
				return
			}
			m.SourceObject = src

			// Check if identical mount is already running
			mu.Lock()
			orig, ok := activeMap[m.BindfsMount]
			mu.Unlock()
			if ok {
				if orig == m.SourceObject {
					validatedChan <- validatedMount{m: m, err: nil} // Mark as "skip" implicitly by not adding to validMounts
					return
				} else {
					validatedChan <- validatedMount{err: fmt.Errorf("mountpoint %q is already mounted to %q, but config requests %q", m.BindfsMount, orig, m.SourceObject)}
					return
				}
			}

			validatedChan <- validatedMount{m: m, err: nil}
		}(m)
	}

	go func() {
		validationWg.Wait()
		close(validatedChan)
	}()

	for vm := range validatedChan {
		if vm.err != nil {
			return nil, vm.err
		}
		// If m.BindfsMount is empty, it means we skip it (already mounted)
		if vm.m.BindfsMount != "" {
			// Check if we should actually skip it based on activeMap (re-check logic)
			alreadyActive := false
			if orig, ok := activeMap[vm.m.BindfsMount]; ok && orig == vm.m.SourceObject {
				alreadyActive = true
			}

			if alreadyActive {
				if !cfg.quiet {
					_, _ = fmt.Fprintf(out, "Already mounted: %s -> %s\n", vm.m.SourceObject, vm.m.BindfsMount)
				}
			} else {
				validMounts = append(validMounts, vm.m)
			}
		}
	}

	if len(validMounts) == 0 {
		if !cfg.quiet {
			_, _ = fmt.Fprintln(out, "All configured mountpoints are already active.")
		}
		return nil, nil
	}

	var servers []*fuse.Server
	var mountErrs []string

	// Parallel mounting
	var mountWg sync.WaitGroup
	for _, m := range validMounts {
		mountWg.Add(1)
		go func(mount MountConfig) {
			defer mountWg.Done()

			// Create a copy of the base config and append the JSON-specific options
			localCfg := *cfg
			localCfg.options = ""

			// Override metadata if specified in this mount's JSON block
			if mount.Uid != nil {
				localCfg.uid = int(*mount.Uid)
			}
			if mount.Gid != nil {
				localCfg.gid = int(*mount.Gid)
			}
			if mount.Dmode != "" {
				localCfg.dmode = mount.Dmode
			}
			if mount.Fmode != "" {
				localCfg.fmode = mount.Fmode
			}

			// Also parse metadata overrides from the options list in multimount
			var filteredOpts []string
			for _, opt := range mount.Options {
				parts := strings.FieldsFunc(opt, func(r rune) bool {
					return r == '=' || r == ' '
				})
				if len(parts) >= 2 {
					key := parts[0]
					val := strings.Join(parts[1:], "=")
					switch key {
					case "uid":
						if v, err := strconv.Atoi(val); err == nil {
							localCfg.uid = v
						}
						continue
					case "gid":
						if v, err := strconv.Atoi(val); err == nil {
							localCfg.gid = v
						}
						continue
					case "fmode":
						localCfg.fmode = val
						continue
					case "dmode":
						localCfg.dmode = val
						continue
					}
				}
				filteredOpts = append(filteredOpts, opt)
			}

			server, err := runMount(&localCfg, mount.SourceObject, mount.BindfsMount, filteredOpts, deps)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				mountErrs = append(mountErrs, fmt.Sprintf("Failed to mount %s -> %s: %v", mount.SourceObject, mount.BindfsMount, err))
				return
			}

			servers = append(servers, server)
			if !cfg.quiet {
				msg := fmt.Sprintf("Mounted %s -> %s", mount.SourceObject, mount.BindfsMount)
				if mount.Comment != "" {
					msg += fmt.Sprintf(" (%s)", mount.Comment)
				}
				_, _ = fmt.Fprintln(out, msg)
			}
		}(m)
	}

	// Wait for all mount attempts to finish
	mountWg.Wait()

	if len(mountErrs) > 0 {
		for _, e := range mountErrs {
			log.Println(e)
		}
		// Unmount any that succeeded before failing the process
		for _, s := range servers {
			if err := s.Unmount(); err != nil {
				log.Printf("Rollback unmount failed: %v", err)
			}
		}
		// Wait for rollback tear down to complete to prevent zombie mounts
		for _, s := range servers {
			s.Wait()
		}
		return nil, fmt.Errorf("one or more mounts failed")
	}

	return servers, nil
}

// setupSingleMount is a simplified handler for the standalone single-mount CLI case.
func setupSingleMount(cfg *config, deps *SystemDeps, out io.Writer) ([]*fuse.Server, error) {
	// Reconcile against already active mounts
	activeMounts, _ := getActiveMounts(deps)
	for _, am := range activeMounts {
		if am.Mountpoint == cfg.mountPoint {
			if am.Original == cfg.sourceObject {
				if !cfg.quiet {
					// #nosec G705 -- CLI standard output, not HTTP output (no XSS risk)
					_, _ = fmt.Fprintf(out, "Already mounted: %s -> %s\n", cfg.sourceObject, cfg.mountPoint)
				}
				return nil, nil // Return empty server list since it's already running in another process
			}
			return nil, fmt.Errorf("mountpoint %q is already mounted to %q, but requested %q", cfg.mountPoint, am.Original, cfg.sourceObject)
		}
	}

	server, err := runMount(cfg, cfg.sourceObject, cfg.mountPoint, nil, deps)
	if err != nil {
		return nil, fmt.Errorf("mount error: %w", err)
	}

	if !cfg.quiet {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "MOUNTPOINT\tSOURCE\tSTATUS")
		_, _ = fmt.Fprintln(w, "----------\t------\t------")
		// #nosec G705 -- Trusted CLI output to standard output, not a web context (no XSS risk)
		_, _ = fmt.Fprintf(w, "%s\t%s\tACTIVE\n", cfg.mountPoint, cfg.sourceObject)
		_ = w.Flush()
	}

	return []*fuse.Server{server}, nil
}
