// SPDX-License-Identifier: GPL-3.0-or-later

// Package config locates cdir's files and reads the optional config file.
//
// The config file uses the same key=value lines as the bookmark file:
//
//	show_hidden=false      # show dot-directories when the browser starts
//	show_files=false       # list regular files (dimmed) alongside directories
//	follow_symlinks=false  # print the resolved physical path on selection
//	sort=natural           # natural ("file2" < "file10") or alpha
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds the user's settings.
type Config struct {
	ShowHidden     bool
	ShowFiles      bool
	FollowSymlinks bool
	Sort           string // "natural" or "alpha"
}

// Default returns the settings used when there is no config file.
func Default() Config {
	return Config{Sort: "natural"}
}

// Dir returns cdir's configuration directory:
// $XDG_CONFIG_HOME/cdir, or ~/.config/cdir.
func Dir(home string) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "cdir")
	}
	return filepath.Join(home, ".config", "cdir")
}

// BookmarksPath returns the bookmark file location.
func BookmarksPath(home string) string { return filepath.Join(Dir(home), "bookmarks") }

// Path returns the config file location.
func Path(home string) string { return filepath.Join(Dir(home), "config") }

// Load reads the config file. A missing file yields the defaults. Unknown
// keys and bad values are reported as warnings and otherwise ignored.
func Load(path string) (Config, []string) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, []string{fmt.Sprintf("config: %v", err)}
	}
	var warns []string
	for i, raw := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || s[0] == '#' {
			continue
		}
		key, val, ok := strings.Cut(s, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !ok {
			warns = append(warns, fmt.Sprintf("config line %d: missing '='", i+1))
			continue
		}
		var bad bool
		switch key {
		case "show_hidden":
			c.ShowHidden, bad = parseBool(val)
		case "show_files":
			c.ShowFiles, bad = parseBool(val)
		case "follow_symlinks":
			c.FollowSymlinks, bad = parseBool(val)
		case "sort":
			if val == "natural" || val == "alpha" {
				c.Sort = val
			} else {
				bad = true
			}
		default:
			warns = append(warns, fmt.Sprintf("config line %d: unknown setting %q", i+1, key))
			continue
		}
		if bad {
			warns = append(warns, fmt.Sprintf("config line %d: bad value %q for %s", i+1, val, key))
		}
	}
	return c, warns
}

func parseBool(s string) (v, bad bool) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "1":
		return true, false
	case "false", "no", "off", "0":
		return false, false
	}
	return false, true
}
