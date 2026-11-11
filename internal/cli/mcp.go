package cli

import (
	"io"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"waypoint/internal/mcp"
)

const usageMCP = "waypoint mcp [--project NAME]"

// cmdMCP serves the project's board over MCP on standard input and output
// until standard input closes. Only protocol messages go to stdout. The
// project is resolved first, so a mistake is reported on stderr and the
// process exits before any protocol traffic.
func (r *runner) cmdMCP(args []string) int {
	pos, code, done := r.parse(usageMCP, args, nil)
	if done {
		return code
	}
	if len(pos) > 0 {
		return r.usageError("mcp takes no arguments", usageMCP)
	}
	r.json = false // errors go to stderr; stdout is for the protocol only
	svc, _, err := r.service(r.project)
	if err != nil {
		return r.fail(err)
	}
	var in io.Reader = strings.NewReader("")
	if r.env.Stdin != nil {
		in = r.env.Stdin
	}
	t := &sdk.IOTransport{Reader: io.NopCloser(in), Writer: nopWriteCloser{r.env.Stdout}}
	if err := mcp.New(svc).Run(r.ctx, t); err != nil && r.ctx.Err() == nil {
		return r.fail(err)
	}
	return exitOK
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
