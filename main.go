package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fuse"
)

// Version can be overwritten during build via ldflags: -X main.Version=...
var Version = "dev"

// setupProfiling enables CPU and memory profiling based on CLI flags.
func setupProfiling(cfg *config, out io.Writer) func() {
	var cpuProfileFile *os.File

	if cfg.cpuProfile != "" {
		// #nosec G304 -- File inclusion via variable is safe here because cpuProfile is sanitized via filepath.Clean in config parser
		var err error
		// #nosec G304 G703 -- File inclusion is safe here because cpuProfile is sanitized via filepath.Clean in config parser
		cpuProfileFile, err = os.Create(cfg.cpuProfile)
		if err != nil {
			log.Fatalf("Could not create CPU profile: %v", err)
		}
		if err := pprof.StartCPUProfile(cpuProfileFile); err != nil {
			log.Fatalf("Could not start CPU profile: %v", err)
		}
		if !cfg.quiet {
			// #nosec G705 -- CLI standard output, not HTTP output (no XSS risk)
			_, _ = fmt.Fprintf(out, "CPU profiling enabled: writing to %s\n", cfg.cpuProfile)
		}
	}

	if cfg.memProfile != "" {
		// Create a dedicated channel for USR1 signals to trigger memory profile dumps
		// without interrupting the main application flow.
		memSigChan := make(chan os.Signal, 1)
		signal.Notify(memSigChan, syscall.SIGUSR1)
		go func() {
			snapshotCount := 0
			for range memSigChan {
				// #nosec G304 -- File inclusion via variable is safe here because memProfile prefix is sanitized via filepath.Clean in config parser
				filename := fmt.Sprintf("%s-%d.memprof", cfg.memProfile, snapshotCount)
				// #nosec G304 -- File inclusion via variable is safe here because memProfile prefix is sanitized via filepath.Clean in config parser
				f, err := os.Create(filename)
				if err != nil {
					log.Printf("Could not create memory profile snapshot %s: %v", filename, err)
					continue
				}
				if err := pprof.WriteHeapProfile(f); err != nil {
					log.Printf("Could not write memory profile snapshot %s: %v", filename, err)
				} else if !cfg.quiet {
					_, _ = fmt.Fprintf(out, "Memory profile snapshot written to %s\n", filename)
				}
				_ = f.Close()
				snapshotCount++
			}
		}()
		if !cfg.quiet {
			// #nosec G705 -- CLI standard output, not HTTP output (no XSS risk)
			_, _ = fmt.Fprintf(out, "Memory profiling enabled: send SIGUSR1 to pid %d to write snapshots to %s-<num>.memprof\n", os.Getpid(), cfg.memProfile)
		}
	}

	return func() {
		if cpuProfileFile != nil {
			pprof.StopCPUProfile()
			_ = cpuProfileFile.Close()
		}
	}
}

// run encapsulates the core program logic so it can be effectively tested
// without triggering hard os.Exit() calls that terminate test runners.
func run(args []string, deps *SystemDeps, out io.Writer) int {
	log.SetFlags(log.Lmicroseconds)
	cfg, err := parseFlags(args)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		_, _ = fmt.Fprintf(out, "Parse error: %v\n", err)
		return 1
	}

	if cfg.version {
		_, _ = fmt.Fprintf(out, "gobindfs version %s\n", Version)
		return 0
	}

	if cfg.listMounts {
		if err := handleListMounts(cfg, deps, out); err != nil {
			log.Printf("List mounts failed: %v", err)
			return 1
		}
		return 0
	}

	if cfg.umount != "" {
		if err := doUnmount(cfg.umount, deps); err != nil {
			log.Printf("Unmount failed: %v", err)
			return 1
		}
		// #nosec G705 -- CLI standard output, not HTTP output (no XSS risk)
		_, _ = fmt.Fprintf(out, "Successfully unmounted %s\n", cfg.umount)
		return 0
	}

	if cfg.umountAll {
		if err := doUnmountAll(deps, out); err != nil {
			log.Printf("Unmount all failed: %v", err)
			return 1
		}
		return 0
	}

	// If requested via the -detach flag, spawn an identical child process
	// that runs asynchronously in a new session, effectively demonizing
	// the application and returning immediate control to the user's terminal.
	if cfg.detach {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("Failed to get executable for detach: %v", err)
			return 1
		}

		var childArgs []string
		for _, arg := range args {
			// Strip all variations of the detach flag to prevent infinite background forking
			if arg != "-detach" && arg != "--detach" && !strings.HasPrefix(arg, "-detach=") && !strings.HasPrefix(arg, "--detach=") {
				childArgs = append(childArgs, arg)
			}
		}

		// #nosec G204 G702 -- Re-executing current binary for detach; arguments are safely passed
		cmd := exec.Command(exe, childArgs...)
		// Create a new session for the background process
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start detached process: %v", err)
			return 1
		}
		return 0
	}

	// Setup performance profiling and ensure resources are cleaned up on exit.
	cleanup := setupProfiling(cfg, out)
	defer cleanup()

	var servers []*fuse.Server

	if cfg.multiMount != "" {
		servers, err = setupMultiMount(cfg, deps, out)
	} else {
		servers, err = setupSingleMount(cfg, deps, out)
	}

	if err != nil {
		log.Printf("Initialization failed: %v", err)
		return 1
	}

	// Setup graceful shutdown mechanism listening for interrupt signals.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		var uwg sync.WaitGroup
		for _, server := range servers {
			uwg.Add(1)
			go func(srv *fuse.Server) {
				defer uwg.Done()
				if err := srv.Unmount(); err != nil {
					log.Printf("Error unmounting: %v", err)
				}
			}(server)
		}
		uwg.Wait()
	}()

	// Block the main thread until all filesystems are unmounted.
	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Add(1)
		go func(srv *fuse.Server) {
			defer wg.Done()
			srv.Wait()
		}(s)
	}
	wg.Wait()

	return 0
}

// main is the entry point. It delegates to run() and captures the exit code.
func main() {
	os.Exit(run(os.Args[1:], defaultSystemDeps(), os.Stdout))
}
