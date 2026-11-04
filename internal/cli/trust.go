package cli

import (
	"fmt"

	"waypoint/internal/server"
)

const usageTrust = "waypoint trust"

// installCA adds the local CA to the system trust store. A variable so tests
// never touch the real one.
var installCA = func(c server.Certs) error { return c.Install() }

// cmdTrust creates the local CA if needed and trusts it, so that "waypoint
// serve" can use HTTPS and with it HTTP/2.
func (r *runner) cmdTrust(args []string) int {
	pos, code, done := r.parse(usageTrust, args, nil)
	if done {
		return code
	}
	if len(pos) != 0 {
		return r.usageError("trust takes no arguments", usageTrust)
	}
	cfg, err := r.loadConfig()
	if err != nil {
		return r.fail(err)
	}
	certs := certsFor(cfg, r.env.Getenv)
	if _, err := certs.Ensure(r.env.Now()); err != nil {
		return r.fail(err)
	}
	if err := installCA(certs); err != nil {
		return r.fail(err)
	}
	fmt.Fprintln(r.env.Stdout, `Trusted the Waypoint local CA. Restart "waypoint serve" to use HTTPS and HTTP/2.`)
	return exitOK
}
