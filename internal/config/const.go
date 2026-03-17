package config

import (
	"fmt"
	"runtime"
)

// Will be embedded at build time.
var (
	version string
	commit  string
)

// BuildInfo returns build information string that can be used for version printing.
func BuildInfo() string {
	v := version
	c := commit

	if v == "" {
		v = "?"
	}

	if c == "" {
		c = "?"
	}

	return fmt.Sprintf("%v %v %v/%v", v, c, runtime.GOOS, runtime.GOARCH)
}
