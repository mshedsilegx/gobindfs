package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
		check     func(*testing.T, *config)
	}{
		{
			name:      "Version flag",
			args:      []string{"-version"},
			wantError: false,
			check: func(t *testing.T, cfg *config) {
				if !cfg.version {
					t.Errorf("Expected version to be true")
				}
			},
		},
		{
			name:      "Missing Positional Arguments",
			args:      []string{},
			wantError: true,
		},
		{
			name:      "Umount flag skips positional arguments",
			args:      []string{"-umount", "/tmp"},
			wantError: false,
			check: func(t *testing.T, cfg *config) {
				if cfg.umount != "/tmp" {
					t.Errorf("Expected umount to be /tmp")
				}
			},
		},
		{
			name:      "Chmod flag parsing",
			args:      []string{"-mount", "/tmp", "-source", "/tmp", "-dmode", "755"},
			wantError: false,
			check: func(t *testing.T, cfg *config) {
				if cfg.dmode != "755" {
					t.Errorf("Expected dmode to be 755, got %s", cfg.dmode)
				}
			},
		},
		{
			name:      "Listmounts flag skips positional arguments",
			args:      []string{"-listmounts"},
			wantError: false,
			check: func(t *testing.T, cfg *config) {
				if !cfg.listMounts {
					t.Errorf("Expected listMounts to be true")
				}
			},
		},
		{
			name:      "Conflicting flag: dmode with multiMount",
			args:      []string{"-multimount", "foo.json", "-dmode", "640"},
			wantError: true,
		},
		{
			name:      "Conflicting flag: options with multiMount",
			args:      []string{"-multimount", "foo.json", "-o", "ro,allow_other"},
			wantError: true,
		},
		{
			name:      "Conflicting flag: ro with multiMount",
			args:      []string{"-multimount", "foo.json", "-ro"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: version and listMounts",
			args:      []string{"-version", "-listmounts"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: umount and umount-all",
			args:      []string{"-umount", "/tmp", "-umount-all"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: config flags with version",
			args:      []string{"-version", "-ro"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: mount flags with version",
			args:      []string{"-version", "-source", "/tmp"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: config flags with umount",
			args:      []string{"-umount", "/tmp", "-ro"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: mount flags with umount",
			args:      []string{"-umount", "/tmp", "-source", "/tmp"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: config flags with listMounts",
			args:      []string{"-listmounts", "-ro"},
			wantError: true,
		},
		{
			name:      "Conflicting commands: mount flags with listMounts",
			args:      []string{"-listmounts", "-source", "/tmp"},
			wantError: true,
		},
		{
			name:      "Conflicting flag: mount with multiMount",
			args:      []string{"-multimount", "foo.json", "-mount", "/tmp"},
			wantError: true,
		},
		{
			name:      "Multimount flag skips positional arguments",
			args:      []string{"-multimount", "dummy.json"}, // will be replaced in loop
			wantError: false,
			check: func(t *testing.T, cfg *config) {
				if !strings.HasSuffix(cfg.multiMount, "dummy.json") {
					t.Errorf("Expected multiMount to be dummy.json")
				}
			},
		},
		{
			name:      "Missing arguments",
			args:      []string{"-detach"},
			wantError: true,
		},
		{
			name:      "Normal valid arguments",
			args:      []string{"-mount", "/tmp/mnt", "-source", "/tmp/src"},
			wantError: false,
			check: func(t *testing.T, cfg *config) {
				if !strings.HasSuffix(cfg.mountPoint, "/tmp/mnt") {
					t.Errorf("Expected mount point /tmp/mnt, got %s", cfg.mountPoint)
				}
			},
		},
		{
			name:      "Using deprecated positional arguments",
			args:      []string{"/tmp/mnt", "/tmp/src"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Multimount flag skips positional arguments" {
				// Use a real file for testing to bypass symlink validation failures
				tmpFile, err := os.CreateTemp("", "dummy.json")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				defer func() {
					_ = os.Remove(tmpFile.Name())
				}()
				tt.args = []string{"-multimount", tmpFile.Name()}
				tt.check = func(t *testing.T, cfg *config) {
					if cfg.multiMount != tmpFile.Name() {
						t.Errorf("Expected multiMount to be %s, got %s", tmpFile.Name(), cfg.multiMount)
					}
				}
			}

			cfg, err := parseFlags(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("parseFlags() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if err == nil && tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
