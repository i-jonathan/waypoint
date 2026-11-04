package config

import "waypoint/internal/domain"

// ResolvedAppearance is the appearance a board is shown with, after the
// project's overrides, the global settings and the built-in defaults have been
// merged. Source names, for each field, which of "project", "global" or
// "default" supplied the value.
type ResolvedAppearance struct {
	Theme    string            `json:"theme"`
	Accent   string            `json:"accent"`
	Density  string            `json:"density"`
	Swimlane string            `json:"swimlane"`
	Textures string            `json:"textures"`
	Source   map[string]string `json:"source"`
}

// Resolve picks each appearance field from the project, else the global
// settings, else the built-in default. An empty value means "not set". Values
// are not validated here: see domain.ValidateAppearance.
func Resolve(global, project domain.Appearance) ResolvedAppearance {
	r := ResolvedAppearance{Source: make(map[string]string, 5)}
	pick := func(field string, out *string, g, p, def string) {
		switch {
		case p != "":
			*out, r.Source[field] = p, "project"
		case g != "":
			*out, r.Source[field] = g, "global"
		default:
			*out, r.Source[field] = def, "default"
		}
	}
	pick("theme", &r.Theme, global.Theme, project.Theme, "system")
	pick("accent", &r.Accent, global.Accent, project.Accent, "orange")
	pick("density", &r.Density, global.Density, project.Density, "comfortable")
	pick("swimlane", &r.Swimlane, global.Swimlane, project.Swimlane, "none")
	pick("textures", &r.Textures, global.Textures, project.Textures, "on")
	return r
}
