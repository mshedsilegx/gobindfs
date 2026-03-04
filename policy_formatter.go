package main

import (
	"fmt"
	"strings"
)

// formatPolicy builds a string representing the active metadata overrides for a mount
func formatPolicy(uid *uint32, gid *uint32, dmode string, fmode string) string {
	var parts []string
	if uid != nil {
		parts = append(parts, fmt.Sprintf("uid=%d", *uid))
	}
	if gid != nil {
		parts = append(parts, fmt.Sprintf("gid=%d", *gid))
	}
	if dmode != "" {
		parts = append(parts, fmt.Sprintf("dmode=%s", dmode))
	}
	if fmode != "" {
		parts = append(parts, fmt.Sprintf("fmode=%s", fmode))
	}

	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

// parsePolicyFromOptions extracts metadata policy from FUSE mount options
func parsePolicyFromOptions(original, options string) (string, string) {
	// 1. Check if policy is encoded in the "Original" (FsName) field
	idx := strings.IndexByte(original, ';')
	if idx != -1 {
		return original[:idx], original[idx+1:]
	}

	// 2. Check FUSE mount options for our custom metadata overrides.
	// We specifically look for "uid=", "gid=", "fmode=", "dmode=" that were
	// passed via -o and are NOT standard FUSE user_id/group_id.
	// We also look for performance flags: "max_read=", "attr_timeout=",
	// "entry_timeout=", "negative_timeout=", and boolean flags like "noatime".
	var parts []string
	opts := strings.Split(options, ",")
	for _, opt := range opts {
		if strings.HasPrefix(opt, "fmode=") || strings.HasPrefix(opt, "dmode=") ||
			strings.HasPrefix(opt, "uid=") || strings.HasPrefix(opt, "gid=") ||
			strings.HasPrefix(opt, "max_read=") ||
			strings.HasPrefix(opt, "attr_timeout=") || strings.HasPrefix(opt, "entry_timeout=") ||
			strings.HasPrefix(opt, "negative_timeout=") || opt == "noatime" {
			parts = append(parts, opt)
		}
	}

	policy := "-"
	if len(parts) > 0 {
		policy = strings.Join(parts, ",")
	}
	return original, policy
}
