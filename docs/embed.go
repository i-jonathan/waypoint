// Package docs embeds the user-facing documentation shipped with Waypoint.
package docs

import _ "embed"

// BoardFormat is the board file format guide, for people and agents that edit
// a board by hand.
//
//go:embed board-format.md
var BoardFormat string
