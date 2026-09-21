package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set by the release build; a build from source says so instead.
var (
	version = ""
	commit  = ""
)

// versionString prefers what the release stamped in, and otherwise asks
// the Go toolchain, which records the revision for anything built from a
// git checkout. A bug report is much easier to place with this in it.
func versionString() string {
	v, c := version, commit
	if v == "" {
		v = "dev"
	}
	if c == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" {
					c = s.Value
				}
			}
		}
	}
	if len(c) > 7 {
		c = c[:7]
	}
	if c == "" {
		return fmt.Sprintf("argus %s (%s/%s)", v, runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("argus %s (%s, %s/%s)", v, c, runtime.GOOS, runtime.GOARCH)
}
