package main

import (
	"fmt"
	"strings"
)

// MountInfo defines a struct to map the current active mountpoint back
// to the source object device or directory path that was mounted.
type MountInfo struct {
	Mountpoint string
	Original   string
	Options    string
}

// getActiveMounts scans the system's `/proc/mounts` file to find currently active
// mountpoints that belong to this gobindfs filesystem.
//
// `/proc/mounts` escapes whitespace and special characters in paths (e.g. space to \040).
// This function parses each line, identifies `fuse.gobindfs` filesystems, and unescapes
// the paths correctly to ensure robust matching against our configuration paths.
func getActiveMounts(deps *SystemDeps) ([]MountInfo, error) {
	data, err := deps.ReadFile("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/mounts: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var activeMounts []MountInfo

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[2] == "fuse."+fsName {
			orig := strings.ReplaceAll(fields[0], "\\040", " ")
			orig = strings.ReplaceAll(orig, "\\011", "\t")
			orig = strings.ReplaceAll(orig, "\\012", "\n")
			orig = strings.ReplaceAll(orig, "\\134", "\\")

			mp := strings.ReplaceAll(fields[1], "\\040", " ")
			mp = strings.ReplaceAll(mp, "\\011", "\t")
			mp = strings.ReplaceAll(mp, "\\012", "\n")
			mp = strings.ReplaceAll(mp, "\\134", "\\")

			activeMounts = append(activeMounts, MountInfo{
				Mountpoint: mp,
				Original:   orig,
				Options:    fields[3],
			})
		}
	}
	return activeMounts, nil
}
