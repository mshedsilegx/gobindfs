# Changelog

All notable changes to this project will be documented in this file.

## [2026-03-03]

### Added
- Added support for metadata overrides (`-uid`, `-gid`, `-fmode`, `-dmode`) to customize file and directory attributes at the mount level.
- Implemented `POLICY` column in `-listmounts` to display active metadata overrides by encoding them in the FUSE source name (`FsName`).
- Added automatic extraction of metadata overrides from the `options` array in multi-mount JSON configurations.
- Added professional table-based mount success messages for both single and multi-mount operations.
- **Performance**: Implemented `sync.Pool` for `OverrideNode` to significantly reduce allocation overhead.
- **Performance**: Implemented custom `NodeReaddirer` with attribute injection to optimize directory listings.
- **Performance**: Added 500ms cache for active mount discovery to improve `-listmounts` responsiveness.
- **Performance**: Parallelized path validation and symlink evaluation in multi-mount setup.

### Fixed
- Fixed a severe nil pointer dereference panic in `OverrideNode.Getattr` caused by double embedding of `fs.Inode`.
- Fixed mount failures with `fusermount3` by filtering out non-standard metadata override flags from FUSE mount options.
- Resolved an issue where the `POLICY` column in `-listmounts` would show `-` for active mounts with metadata overrides.
- Updated `OverrideNode` to correctly wrap child nodes and propagate metadata overrides across the entire filesystem tree.
- **Performance**: Optimized policy parsing logic to minimize string allocations.

## [2026-03-02]

### Added
- Added `-chmod` flag to enforce octal file permissions via restrictive FUSE umask overrides on single mounts.
- Added `chmod` property to the JSON configuration to support restrictive umask overrides in multi-mount environments.
- Added `MASK` column to the `-listmounts` output to dynamically display the active `chmod` permission mask based on parsed `umask` configurations.
- Deprecated positional arguments for mounting in favor of explicit `-mount <mountpoint>` and `-source <source_object>` flags for improved clarity and safety.
- Introduced robust validation in `parseFlags` to identify and securely reject logically conflicting flags (e.g. attempting to pass `-chmod` alongside `-multimount`, or `-ro` alongside `-version`).

### Changed
- Refactored internal `config` struct properties to strictly adhere to Go's standard camelCase naming conventions.
- Updated all `README.md` examples and documentation to strictly use explicit `-mount` and `-source` flag syntax instead of positional arguments.

## [2026-03-01]

### Added
- Added `SOURCE OBJECT` column to the `-listmounts` output to display the underlying directory being mounted.
- Added `-detach` flag to optionally run the gobindfs process in the background and return control to the shell prompt immediately.

### Changed
- Improved error handling in `doUnmount` to provide a clean, user-friendly error message when a mountpoint is locked or busy (`EBUSY`), instead of dumping the raw fallback errors.
- Modified `getActiveMounts` to extract the underlying original path in addition to the mountpoint from `/proc/mounts`.

## [2026-02-27]

### Added
- Implemented `-listmounts` utility flag to display a nicely formatted table of all active gobindfs mounts currently tracked by the system.
- Enhanced `-listmounts` to accept `-multimount <config.json>` for reconcilation, throwing errors for any missing or orphaned mounts.
- Implemented `-o` flag to pass low-level FUSE options directly (e.g. `-o allow_other,ro,max_read=131072`) for performance tuning under heavy load.
- Added `Options` array to the `MountConfig` JSON structure to allow passing custom FUSE directives in multi-mount environments.
- Implemented parallel multi-mounting via the `-multimount <gobindfs_mounts.json>` flag, using `sync.WaitGroup` to handle concurrent FUSE mounts safely.
- Added `-umount <mountpoint>` utility flag to safely tear down a specific FUSE mount without relying directly on shell commands.
- Added `-umount-all` utility flag to discover and gracefully unmount all active gobindfs mounts by reading from `/proc/mounts`.
- Added `-version` command-line flag and `main.Version` variable (overridable via `ldflags`) to check application version.
- Comprehensive inline documentation added to `main.go` describing core concepts, structs, and logic flows.
- `README.md` added with architecture overview, dependencies, command-line arguments, and usage examples.
- `CHANGELOG.md` file created to track project changes.
- Extracted and defined explicit package constants for magic strings (`fsName`, `optDefaultPerms`, `optReadOnly`).
- Extracted `config` struct to encapsulate flag data and arguments cleanly.
- Added path sanitization (`filepath.Clean`) and absolute path resolution (`filepath.Abs`) for input directory paths.
- Added pre-flight `os.Stat` checks to verify original and mountpoint directories exist before attempting to mount FUSE.
- `bufio.NewWriter` wrapper added to the CPU profiler to reduce unbuffered I/O overhead.

### Changed
- Renamed project references from `loopback` to `gobindfs` across source code and documentation.
- Refactored the monolithic `main()` function into modular `parseFlags()`, `setupProfiling()`, and `runMount()` functions for improved maintainability.
- Changed FUSE signal handling channel to be buffered (`make(chan os.Signal, 1)`) to avoid dropping OS interrupt signals.
- Custom `flag.Usage` function mapped properly so `./loopback --help` renders custom instruction strings rather than Go's defaults.
- Updated CPU profiling defer block to correctly close file descriptors to prevent resource leaks (`f.Close()`).
- Error handling in the async unmount routine now correctly logs `server.Unmount()` failures instead of silently ignoring them.

### Fixed
- Resolved zombie mount race condition on multi-mount failure: Added `Wait()` synchronization and proper error logging when rolling back `Unmount()` operations.
- Mitigated symlink-based path traversal attacks (TOCTOU) by adding `filepath.EvalSymlinks` to all user-provided file paths before `os.Stat` validations.
- Fixed shadowing bug in `writeMemProfile` where variable `fn` was reused in inner loop. Inner variable renamed to `profFile`.
- Fixed open file descriptor leak when starting a CPU profile.
