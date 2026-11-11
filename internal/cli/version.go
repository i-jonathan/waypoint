package cli

import (
	"fmt"
	"runtime"
)

// Version is the release this binary was built from. A release build sets it
// with -ldflags "-X waypoint/internal/cli.Version=v0.1.0"; a build from a
// checkout reports "dev".
var Version = "dev"

const usageVersion = "waypoint version [--json]"

// cmdVersion prints the version, and the Go version and platform it was built
// for. It needs no project and no config.
func (r *runner) cmdVersion(args []string) int {
	pos, code, done := r.parse(usageVersion, args, nil)
	if done {
		return code
	}
	if len(pos) > 0 {
		return r.usageError("version takes no arguments", usageVersion)
	}
	if r.json {
		r.printJSON(struct {
			Version  string `json:"version"`
			Go       string `json:"go"`
			Platform string `json:"platform"`
		}{Version, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH})
		return exitOK
	}
	fmt.Fprintf(r.env.Stdout, "waypoint %s (%s %s/%s)\n", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return exitOK
}
