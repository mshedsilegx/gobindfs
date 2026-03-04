package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"text/tabwriter"
	"time"
)

var (
	mountsCache      []MountInfo
	mountsCacheTime  time.Time
	mountsCacheMutex sync.Mutex
)

const cacheDuration = 500 * time.Millisecond

// getActiveMountsCached is a test-friendly version of getActiveMounts that uses a cache.
// In tests, we need to be careful with the cache as different tests might expect different /proc/mounts content.
func getActiveMountsCached(deps *SystemDeps) ([]MountInfo, error) {
	// If we are in a test environment (detected by deps being a mock), we might want to skip caching
	// or ensure the cache is invalidated. For simplicity in this production code, we'll keep the cache
	// but it's important to know that mocks might need to handle this.
	mountsCacheMutex.Lock()
	defer mountsCacheMutex.Unlock()

	if time.Since(mountsCacheTime) < cacheDuration && mountsCache != nil {
		return mountsCache, nil
	}

	mounts, err := getActiveMounts(deps)
	if err != nil {
		return nil, err
	}

	mountsCache = mounts
	mountsCacheTime = time.Now()
	return mounts, nil
}

// ResetMountsCache clears the active mounts cache. Useful for testing.
func ResetMountsCache() {
	mountsCacheMutex.Lock()
	defer mountsCacheMutex.Unlock()
	mountsCache = nil
	mountsCacheTime = time.Time{}
}

// handleListMounts prints a table of active gobindfs mounts and reconciles
// them against a multiMount config if provided.
func handleListMounts(cfg *config, deps *SystemDeps, out io.Writer) error {
	activeMounts, err := getActiveMountsCached(deps)
	if err != nil {
		return fmt.Errorf("failed to retrieve active mounts: %w", err)
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ACTIVE MOUNTPOINT\tSOURCE OBJECT\tSTATUS\tPOLICY")
	_, _ = fmt.Fprintln(w, "-----------------\t---------------\t------\t------")

	activeMap := make(map[string]MountInfo)
	for _, m := range activeMounts {
		activeMap[m.Mountpoint] = m
	}

	if cfg.multiMount != "" {
		// #nosec G304 G703 -- Trusted user CLI flag for config file path
		data, err := deps.ReadFile(cfg.multiMount)
		if err != nil {
			return fmt.Errorf("failed to read multiMount config: %w", err)
		}
		var mounts []MountConfig
		if err := json.Unmarshal(data, &mounts); err != nil {
			return fmt.Errorf("failed to parse multiMount config: %w", err)
		}

		hasDiscrepancy := false
		for _, m := range mounts {
			mp, err := filepath.Abs(filepath.Clean(m.BindfsMount))
			if err != nil {
				return fmt.Errorf("invalid bindfs mount %q: %w", m.BindfsMount, err)
			}
			if evaluatedMp, err := filepath.EvalSymlinks(mp); err == nil {
				mp = evaluatedMp
			}

			src, err := filepath.Abs(filepath.Clean(m.SourceObject))
			if err == nil {
				if evaluatedSrc, err := filepath.EvalSymlinks(src); err == nil {
					src = evaluatedSrc
				}
			}

			policyStr := formatPolicy(m.Uid, m.Gid, m.Dmode, m.Fmode)

			if activeMnt, ok := activeMap[mp]; ok {
				cleanedSrc, encodedPolicy := parsePolicyFromOptions(activeMnt.Original, activeMnt.Options)
				if cleanedSrc == src {
					displayPolicy := policyStr
					if encodedPolicy != "-" {
						displayPolicy = encodedPolicy
					}
					_, _ = fmt.Fprintf(w, "%s\t%s\tACTIVE\t%s\n", mp, cleanedSrc, displayPolicy)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t%s\tMISMATCH (expected %s)\t%s\n", mp, cleanedSrc, src, policyStr)
					hasDiscrepancy = true
				}
				delete(activeMap, mp)
			} else {
				_, _ = fmt.Fprintf(w, "%s\t%s\tMISSING\t%s\n", mp, src, policyStr)
				hasDiscrepancy = true
			}
		}

		for mp, activeMnt := range activeMap {
			cleanedSrc, _ := parsePolicyFromOptions(activeMnt.Original, activeMnt.Options)
			_, _ = fmt.Fprintf(w, "%s\t%s\tORPHANED (Not in config)\t-\n", mp, cleanedSrc)
			hasDiscrepancy = true
		}

		_ = w.Flush()

		if hasDiscrepancy {
			return fmt.Errorf("discrepancies found between active mounts and config")
		}
		return nil
	}

	for _, m := range activeMounts {
		cleanedSrc, encodedPolicy := parsePolicyFromOptions(m.Original, m.Options)
		_, _ = fmt.Fprintf(w, "%s\t%s\tACTIVE\t%s\n", m.Mountpoint, cleanedSrc, encodedPolicy)
	}
	_ = w.Flush()

	return nil
}
