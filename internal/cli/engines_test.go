package cli

import (
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/storage/servertest"
)

// engine is one database server kind the server tests run against.
type engine struct {
	name  string
	fresh func(*testing.T) config.ProjectRef
}

var engines = []engine{{"postgres", servertest.Postgres}, {"mysql", servertest.MySQL}}

// eachEngine runs a test once per engine, as a subtest named for it.
func eachEngine(t *testing.T, fn func(t *testing.T, e engine)) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) { fn(t, e) })
	}
}
