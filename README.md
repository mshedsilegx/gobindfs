# Go-FUSE Gobindfs Filesystem

## Application Overview and Objectives

The Go-FUSE Gobindfs Filesystem is a program that uses FUSE (Filesystem in Userspace) to create a pass-through filesystem. It mirrors an existing directory tree to a new mountpoint, shunting all file operations directly to the underlying file system. 

This project serves as a reference implementation for `github.com/hanwen/go-fuse/fs/` and provides a foundation for developers looking to build more complex, layered, or intercepting filesystems on top of existing directories.

## Architecture and Design Choices

The program is designed to be lightweight, modular, and performant:

- **Pass-through Architecture**: It utilizes `fs.NewLoopbackRoot` from `go-fuse` to create a transparent translation layer. Every FUSE operation is directly mapped to its corresponding system call on the original directory.
- **Recursive Node Wrapping**: When users provide `-uid`, `-gid`, `-dmode`, or `-fmode` arguments, the underlying loopback directory structure is recursively wrapped in a custom `OverrideNode`. This node intercepts kernel metadata calls (`Getattr`, `Lookup`) to aggressively enforce file and directory permissions in memory, effectively hijacking the raw disk attributes without incurring any additional system calls or disk I/O.
- **Modular Codebase**: The project is split into focused files to ensure production-grade quality, testability, and ease of maintenance.

### Project Modules

| Module | Primary Responsibility | Key Functions/Structs | Relationship |
|---|---|---|---|
| `main.go` | Application entrypoint and lifecycle | `main()`, `run()`, `setupProfiling()` | Coordinates setup, signal trapping, detached execution, and profiling. Delegates to sub-modules. |
| `config.go` | Configuration and CLI parsing | `config`, `MountConfig`, `parseFlags()` | Validates user inputs and normalizes parameters for the mount engine. |
| `mount_setup.go` | FUSE instantiation | `runMount()`, `setupMultiMount()`, `setupSingleMount()` | Evaluates physical paths, binds go-fuse internals (`fs.Options`), and injects custom Node wrappers if needed. |
| `override_node.go` | Metadata manipulation | `OverrideNode`, `overrideAttr()` | Implements `fs.NodeWrapChilder` to hijack and rewrite filesystem attributes. Attached dynamically by `mount_setup.go`. |
| `mount_info.go` | State discovery | `getActiveMounts()`, `MountInfo` | Parses `/proc/mounts` to identify running `gobindfs` mounts. Relied upon by unmount and list routines. |
| `list_mounts.go` | Mount reconciliation reporting | `handleListMounts()` | Pulls active state from `mount_info.go`, compares it against `config.go` configurations, and prints standard output tables. |
| `policy_formatter.go` | String utility | `formatPolicy()` | Generates human-readable permission policy strings for the `list_mounts.go` output. |
| `unmount.go` | Graceful teardown | `doUnmount()`, `doUnmountAll()` | Cascades through `fusermount3`, `fusermount`, and `syscall.Unmount` safely. Triggered by `main.go`. |
| `systemdeps.go` | Testability abstraction | `SystemDeps`, `defaultSystemDeps()` | Mocks standard OS system calls (read files, execute binaries) allowing 100% deterministic unit testing without root access. |

- **Robustness**: 
  - Validates and sanitizes all input paths (`filepath.Clean`, `filepath.Abs`) to prevent directory traversal issues.
  - Implements pre-flight checks to ensure directories exist before attempting to mount, preventing opaque FUSE failures.
  - Features graceful shutdown mechanisms via OS signal capturing (`os.Interrupt`, `syscall.SIGTERM`).
- **Performance Profiling**: Built-in support for CPU and Memory profiling via `runtime/pprof`, with buffered I/O designed to minimize profiling overhead under heavy filesystem load.

## OS Dependencies

To run `gobindfs` successfully, the host environment must meet the following operating system and utility requirements:

### FUSE Utilities
`gobindfs` relies on standard FUSE helper binaries for lifecycle management, with priority given to FUSE 3:
- **`fusermount3`**: The primary utility for unmounting on modern Linux distributions (`fuse3` distribution package). This is the **preferred** and first-attempted method.
- **`fusermount`**: A legacy fallback for older environments (`fuse` package).
- **`fuse.conf`**: To use the `-allow-other` flag, `/etc/fuse.conf` must contain the `user_allow_other` directive.

### Kernel Requirements
- **FUSE Module**: The Linux `fuse` kernel module must be loaded (`modprobe fuse`).
- **Proc Filesystem**: Access to `/proc/mounts` is required for active mount discovery and reconciliation.
- **ID-Mapped Mounts**: The `-idmapped` feature requires **Linux Kernel 5.12** or higher.

### System Permissions & Calls
- **Mount Privileges**: Establishing FUSE mounts typically requires root privileges or membership in the `fuse` group, depending on distribution security policies.
- **Direct Mounting**: The `-directmount` and `-directmountstrict` flags attempt to use the `mount()` system call directly, which often requires `CAP_SYS_ADMIN` capabilities.
- **Process Detachment**: The `-detach` feature uses `Setsid` to create a new session, ensuring the filesystem remains active after the parent shell exits.

## Applications Dependencies

To run and compile this application, you will need:
- **Go**: Version 1.18 or higher is recommended.
- **FUSE**: The host OS must support FUSE (e.g., `libfuse` or `libfuse3` on Linux, `macfuse` or `osxfuse` on macOS).
- **Go-FUSE Library**: 
  - `github.com/hanwen/go-fuse/v2/fs`
  - `github.com/hanwen/go-fuse/v2/fuse`

## Command Line Arguments

The application accepts the following command line flags. Positional arguments have been deprecated in favor of explicit flags.

### Standalone Operations (No Flags Required)
You can run these commands independently. They will execute and exit immediately:
- `./gobindfs -version`: Prints the binary version.
- `./gobindfs -listmounts`: Prints a formatted table of active `gobindfs` mounts on the system.
- `./gobindfs -umount <dir>`: Safely attempts to unmount the specified directory.
- `./gobindfs -umount-all`: Discovers and safely tears down all active `gobindfs` mounts.

### Single Mount Mode
If not using multi-mount, you must provide these arguments. **Note: Both directories must exist before running gobindfs.**
- `-mount <dir>` *(Required)*: The location of the chroot (the empty directory where the gobindfs filesystem will be exposed). This must be an existing, valid directory.
- `-source <dir>` *(Required)*: The source object (directory) to virtually reference under the chroot. This must be an existing, valid directory.

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-mount` | `string` | `""` | The absolute path to the mountpoint directory. |
| `-source` | `string` | `""` | The absolute path to the original source directory. |
| `-multimount` | `string` | `""` | Path to a JSON configuration file for parallel multi-mounting. If provided, `-mount` and `-source` are ignored. |
| `-listmounts` | `bool` | `false` | Displays a table of active gobindfs mounts. If combined with `-multimount`, it reconciles the active mounts against the configuration. |
| `-o` | `string` | `""` | Comma separated list of low-level FUSE options for performance tuning (e.g. `allow_other,ro,max_read=262144,attr_timeout=0.5`). Only used in single mount mode. |
| `-detach` | `bool` | `false` | Put the process in the background and return control to the prompt. |
| `-debug` | `bool` | `false` | Prints detailed FUSE debugging messages to stderr. |
| `-allow-other` | `bool` | `false` | Mounts with the `-o allow_other` option, allowing other users to access the mountpoint. (Requires `user_allow_other` in `/etc/fuse.conf`). |
| `-idmapped` | `bool` | `false` | Enables id-mapped mounts (Linux specific advanced feature). |
| `-quiet` | `bool` | `false` | Quiet mode. Suppresses diagnostic logging and standard output messages. |
| `-ro` | `bool` | `false` | Mounts the filesystem in read-only mode. |
| `-directmount` | `bool` | `false` | Attempts to call the mount syscall directly instead of executing the `fusermount3` or `fusermount` binary. |
| `-directmountstrict` | `bool` | `false` | Like `-directmount`, but explicitly fails if the direct syscall is unsuccessful, without falling back to `fusermount`. |
| `-umount` | `string` | `""` | Safely unmounts a specific gobindfs mountpoint and exits. Tries `fusermount3`, `fusermount`, and `syscall.Unmount` in order. |
| `-umount-all` | `bool` | `false` | Discovers and safely unmounts all active gobindfs mounts on the system and exits. |
| `-cpuprofile` | `string` | `""` | File path to write the CPU profile to (e.g., `cpu.prof`). |
| `-memprofile` | `string` | `""` | File path prefix to write the memory profile to. Snapshots are triggered by sending `SIGUSR1` to the process. |
| `-uid` | `int` | `-1` | Enforces a specific UID on all files and directories within the mount, overriding the underlying source metadata. |
| `-gid` | `int` | `-1` | Enforces a specific GID on all files and directories within the mount, overriding the underlying source metadata. |
| `-dmode` | `string` | `""` | Enforces a specific octal permission mode exclusively on directories (e.g., `0755`), overriding the underlying metadata. |
| `-fmode` | `string` | `""` | Enforces a specific octal permission mode exclusively on regular files and symlinks (e.g., `0644`), overriding the underlying metadata. |

### Multimount Config File (JSON)
Multiple mounts can be defined in a single JSON file. Running the `gobindfs` command with `-multimount` will parallelize the mounting process. 
**Note:** The multimount process is idempotent. If the command is run again, `gobindfs` will automatically detect the already-active mounts, skip them, and only establish the newly added mounts.

## Platform Considerations & Performance

There are important architectural aspects to keep in mind when deciding to use `gobindfs` in a production or high-throughput environment:

### Multi-Threading and Concurrency
File operations on a `gobindfs` mount are **inherently multi-threaded**.
Because `gobindfs` is built on top of the `hanwen/go-fuse/v2` library and the Go runtime, it takes full advantage of Go's concurrency model:
- When the Linux kernel sends multiple concurrent file operation requests, the FUSE server automatically spawns goroutines to handle them in parallel.
- The Go runtime seamlessly multiplexes these goroutines across available CPU cores. This allows independent operations (e.g., reading from `file A` and writing to `file B`) to happen simultaneously without blocking the main execution thread.

### Built-in Performance Optimizations
`gobindfs` includes several architectural optimizations designed to minimize the FUSE overhead:
- **Recursive Metadata Inlining**: When using metadata overrides, `gobindfs` utilizes a custom `NodeReaddirer` that injects overridden attributes directly into the directory stream. This prevents the kernel from having to make subsequent `Getattr` calls for every file in a directory listing, drastically speeding up `ls -l` operations.
- **Memory Pooling**: High-frequency metadata nodes are managed via a `sync.Pool`, significantly reducing GC pressure and memory allocation overhead during deep directory traversals.
- **Active Mount Caching**: Discovery of active mounts (via `/proc/mounts`) is cached for 500ms, making status checks and reconciliation near-instantaneous even in high-density environments.
- **Parallel Validation**: Multi-mount configurations utilize concurrent "pre-flight" checks, parallelizing path resolution and symlink evaluation to ensure rapid startup regardless of the number of mountpoints.
- **Optimized Defaults**: Default kernel timeouts are set to `0.5s` and the maximum read payload is tuned to `256KB` to provide a high-performance experience out-of-the-box.

### Performance Penalty vs. Native Folders
There is always a performance penalty when using a FUSE (Filesystem in Userspace) bind mount compared to directly accessing a native folder. FUSE requires the kernel to communicate with the `gobindfs` userspace program for every uncached file operation.
- **I/O Throughput:** Expect roughly a 10% to 30% reduction in raw sequential throughput compared to native disk access due to context-switching and buffer copying. *Mitigation: Increase the `max_read` payload size (e.g., `-o max_read=1048576`).* `gobindfs` defaults to `256KB`.
- **Metadata Latency:** Operations like `ls -l` on massive directories can be measurably slower, as each file triggers a `stat` round-trip to userspace. *Mitigation: Use aggressive kernel caching options (`-o attr_timeout=5,entry_timeout=5`).* `gobindfs` defaults to `0.5s`.
- **CPU Overhead:** The `gobindfs` process requires moderate CPU usage under heavy load to marshal data back and forth, though this is rarely a bottleneck on modern multi-core servers.

---

## Deep Dive: Complex Flags Explained

Understanding some of the more advanced flags is critical for running gobindfs safely in production or highly concurrent environments:

### `-allow-other`
By default, FUSE restricts access to the mountpoint strictly to the user who executed the mount command. Even the `root` user cannot read files mounted by a non-root user without this flag.
- **What it does:** Appends the FUSE option `allow_other` to the mount request, which tells the kernel to bypass this restriction, allowing other system users (including `root`) to access the exposed files.
- **Scenario:** Use this when a background daemon (like a web server running as `www-data`) needs to serve files that were mounted by an initialization script running as `root` or a different user. 
- **Prerequisite:** The system administrator must uncomment `user_allow_other` in the `/etc/fuse.conf` file for this flag to be accepted by the kernel.

### `-directmount` and `-directmountstrict`
Normally, FUSE applications execute a setuid helper binary (`fusermount3` or `fusermount`) to establish the connection between the userspace program and the kernel. This adds a slight overhead and relies on external system binaries.
- **What it does (`-directmount`):** Instructs go-fuse to bypass the helper binary entirely and invoke the `mount()` system call directly. If the direct syscall fails (usually due to lack of privileges), it automatically falls back to executing the helper binary.
- **What it does (`-directmountstrict`):** Similar to `-directmount`, but enforces strict mode: if the direct system call fails, the application aborts immediately rather than attempting the fallback.
- **Scenario:** Use `-directmountstrict` in containerized (Docker/Kubernetes) or heavily locked-down environments where executing external setuid binaries is prohibited or blocked by AppArmor/SELinux policies, ensuring predictable and secure initialization.

### `-idmapped`
Linux 5.12 introduced "ID-mapped mounts", allowing the translation of user and group IDs between different mount namespaces without having to recursively `chown` underlying files.
- **What it does:** Signals to the underlying FUSE implementation that the mount point is operating within an ID-mapped namespace environment.
- **Scenario:** Ideal for system administrators sharing a single host directory among multiple unprivileged Linux containers (LXC/LXD/Docker) where each container uses a different range of User namespaces.

### `-cpuprofile` and `-memprofile`
These flags expose standard Go runtime performance profiling tools, designed for continuous profiling without disrupting the active mount.
- **`-cpuprofile <file>`:** Starts CPU profiling immediately upon mounting and continuously writes the data to the specified file. The data buffer is cleanly flushed only when the filesystem is properly unmounted. Use `go tool pprof <file>` to identify CPU bottlenecks under heavy load.
- **`-memprofile <prefix>`:** Unlike CPU profiling, memory profiling is interactive. The application waits in the background. Sending a `SIGUSR1` signal to the gobindfs process ID (e.g., `kill -SIGUSR1 <pid>`) triggers a heap snapshot to be written to `<prefix>-0.memprof`. Subsequent signals write to `-1`, `-2`, allowing you to track memory leaks over time without stopping the application.

---

## Low-Level FUSE Tuning (-o)

The `-o` flag allows passing native FUSE options directly to the kernel to optimize the mount's behavior. In a multi-mount JSON configuration, these can be supplied as a string array in the `"options"` key.

**Important Note on Option Routing:** `gobindfs` automatically inspects these options. Standard VFS flags (like `ro`, `allow_other`, `default_permissions`) are forwarded to the `fusermount3` helper. Kernel protocol settings (like timeouts and payload limits) are intercepted and negotiated directly via the `go-fuse` subsystem during initialization to prevent `fusermount3` from rejecting them.

Consider these options for tuning performance and robustness:

| Option | Routing | Effect on Performance and Robustness |
|--------|---------|--------------------------------------|
| `max_read=<bytes>`<br>`max_write=<bytes>` | **Kernel** | Increases the maximum payload size of a single read/write request. Default is  `262144` (256KB). Increasing this further to `1048576` (1MB) on kernels that support it can improve throughput for large file streaming. |
| `max_pages=<num>` | **Kernel** | *Note: Ignored.* `go-fuse` calculates this automatically based on `max_write`. |
| `attr_timeout=<seconds>` | **Kernel** | Instructs the kernel to cache file attributes (size, permissions) for the given duration. E.g., `attr_timeout=5`. If the underlying source files rarely change, setting this higher significantly reduces the number of round-trips to userspace, vastly improving `ls` and `stat` performance. Default is `0.5`. |
| `entry_timeout=<seconds>` | **Kernel** | Caches the existence (or non-existence) of files within a directory. Paired with `attr_timeout`, this prevents redundant lookups. Setting it to `1.0` or higher provides a major speed boost for deep directory traversals. Default is `0.5`. |
| `negative_timeout=<seconds>` | **Kernel** | Caches failed lookups (ENOENT). Useful if applications frequently check for the existence of missing files. |
| `default_permissions` | **Userspace** | Offloads permission checking (ACLs, read/write/execute bits) to the kernel rather than delegating every single check to the userspace process. This drastically reduces context-switching overhead and is highly recommended for standard file sharing. |
| `ro` | **Userspace** | Mounts the file system entirely as read-only. Beyond preventing accidental modifications, it allows the kernel to aggressively cache read operations without worrying about synchronization or dirty buffers. |
| `noatime` | **Userspace** | Disables updating the last access time on files and directories. This reduces disk write operations and can significantly improve read performance on some storage backends. |

### Recommended Performance Values (Average I/O MFT Load)

For environments with average Managed File Transfers (MFT) load and standard I/O patterns, the following values are recommended for optimal performance:

- `max_read=1048576` (1MB)
- `attr_timeout=30.0` (30 seconds)
- `entry_timeout=30.0` (30 seconds)
- `negative_timeout=5.0` (5 seconds)
- `noatime`

Other FUSE high-performance features are: `writeback_cache` and `async_read`. They are both kernel-level optimizations that reduce context-switching overhead. Writeback Cache buffers writes in the kernel, acknowledging the application immediately while sending data to your Go daemon in large background chunks—boosting write speeds by up to 10x. Async Read allows the kernel to pre-fetch data by sending multiple read requests in parallel, essential for modern storage performance. These are not exposed as configurable options because they are negotiated internally via a bitmask handshake during the INIT phase, thus change the filesystem's safety posture.

---

## Systemd Configuration

To run `gobindfs` automatically as a background service managed by systemd, you need to configure a unit file, adjust FUSE permissions, and set the appropriate mount options.

### 1. Systemd Unit File

Create a service file at `/etc/systemd/system/gobindfs.service`:

```ini
[Unit]
Description=gobindfs multi-mount service
After=local-fs.target network-online.target sys-fs-fuse-connections.mount remote-fs.target
Before=shutdown.target
ConditionPathExists=/etc/gobindfs/config.json
StartLimitIntervalSec=500
StartLimitBurst=5

[Service]
Type=simple
User=<unprivileged_user>
Group=<unprivileged_group>

# Optional: Add security limits if you expect high concurrent usage
LimitNOFILE=65535

# Start gobindfs in the foreground using the multi-mount JSON configuration
ExecStart=/usr/bin/gobindfs -multimount /etc/gobindfs/config.json
ExecStop=/usr/bin/gobindfs -umount-all

# Graceful restart on unexpected failure
Restart=on-failure
RestartSec=30s

[Install]
WantedBy=multi-user.target
```

**Unit Design:**
- `Type=simple` with foreground execution (`ExecStart` without `-detach`) allows systemd to correctly monitor the process state.
- `ExecStop` ensures all active mounts are safely torn down when the service stops.
- Running as a non-root user (`User=<unprivileged_user>` and `Group=<unprivileged_group>`) improves security by dropping unnecessary privileges.

### 2. FUSE Configuration (`/etc/fuse.conf`)

Ensure the following line is uncommented in `/etc/fuse.conf`:

```text
user_allow_other
```

**Access Control Rationale:**
By default, FUSE restricts mount access exclusively to the process that created it. Because systemd runs services within their own isolated control groups (cgroups) and namespaces, even the exact same user (e.g., `<unpriviledged_user>`) logging in via a separate terminal session would be completely denied access to the mounted files without this flag. Enabling `user_allow_other` at the system level permits the use of the `allow_other` FUSE option to bypass this strict isolation.

### 3. Gobindfs Configuration (`/etc/gobindfs/config.json`)

When defining your mounts in the JSON configuration, include the `allow_other` and `default_permissions` options:

```json
[
  {
    "bindfs_mount": "/path/to/mount",
    "source_object": "/path/to/source",
    "options": ["allow_other", "default_permissions", "attr_timeout=5.0", "entry_timeout=5.0"]
  }
]
```

**Configuration Enforcement:**
- `allow_other`: Instructs the FUSE mount to utilize the `user_allow_other` system capability. Without this, systemd's cgroup isolation would prevent any process outside the immediate service unit from accessing the mountpoint—even processes owned by the `<unpriviledged_user>` user.
- `default_permissions`: Offloads standard Linux permission checking (read/write/execute bits) to the kernel rather than delegating it to the `gobindfs` userspace process. This is critical for security and performance when sharing files among multiple users.
- `attr_timeout` / `entry_timeout`: Negotiates high-performance kernel caching protocols directly via the `go-fuse` handler. This allows the kernel to heavily cache directory listings and metadata, reducing costly round-trips to userspace.

---

## Examples

### Multi-Mounting via JSON Configuration
You can mount multiple directories in parallel using a JSON configuration file. 
This is especially useful for managing complex environments, as it allows setting specific low-level FUSE options on a per-mount basis.

Create a file named `/etc/gobindfs/config.json` with the following content (see systemd section for more information):
```json
[
  {
    "bindfs_mount": "/var/www/html/site1",
    "source_object": "/mnt/site1_sandbox",
    "comment": "Primary sandboxed web directory with overridden ownership",
    "uid": 33,
    "gid": 33,
    "dmode": "0755",
    "fmode": "0644",
    "options": ["allow_other", "max_read=1048576", "default_permissions", "noatime"]
  },
  {
    "bindfs_mount": "/var/backups",
    "source_object": "/mnt/backups_ro",
    "comment": "Read-only backup view",
    "options": ["ro", "allow_other"]
  },
  {
    "bindfs_mount": "/home/user/data",
    "source_object": "/home/user/public_view"
  }
]
```

Run the application using the `-multimount` flag pointing to your newly created file:
```bash
./gobindfs -multimount /etc/gobindfs/config.json
```

### Running in the Background
Mount the directory and immediately detach the process to run in the background:
```bash
./gobindfs -detach /tmp/mnt /tmp/source
```

### Listing and Reconciling Mounts
To see all currently active gobindfs mounts in a formatted table:
```bash
./gobindfs -listmounts
```

To reconcile the active mounts against your configuration file to ensure there are no missing or orphaned mounts:
```bash
./gobindfs -listmounts -multimount /etc/gobindfs/config.json
```

### Unmounting
Safely unmount a specific active gobindfs mount:
```bash
./gobindfs -umount /tmp/mnt1
```

Or safely unmount *all* active gobindfs mounts on the system:
```bash
./gobindfs -umount-all
```

### Basic Single Mount
Mirror the `/tmp/source` directory to `/tmp/mnt`:
```bash
mkdir -p /tmp/source /tmp/mnt
echo "hello world" > /tmp/source/hello.txt
./gobindfs -mount /tmp/mnt -source /tmp/source
```
*You can now read `/tmp/mnt/hello.txt`.*

### Performance Tuning with FUSE Options
To tune performance under heavy load, pass specific FUSE parameters using `-o`. For instance, to further increase the maximum read payload size, tune caching timeouts, and disable access time updates:
```bash
./gobindfs -o max_read=1048576,attr_timeout=5.0,entry_timeout=5.0,noatime -mount /tmp/mnt -source /tmp/source
```

### Read-Only Mount
Mount the directory so no modifications can be made through the FUSE mount:
```bash
./gobindfs -ro -mount /tmp/mnt -source /tmp/source
```

### Allowing Other Users
Allow other system users to access the FUSE mount:
```bash
./gobindfs -allow-other -mount /tmp/mnt -source /tmp/source
```

### Debugging & Profiling
Run with verbose FUSE logs and capture a CPU profile for performance analysis:
```bash
./gobindfs -debug -cpuprofile=cpu.prof -mount /tmp/mnt -source /tmp/source
```

### Checking Version
You can check the compiled version of the binary:
```bash
./gobindfs -version
```
