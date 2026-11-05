package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"waypoint/internal/domain"
)

func TestResolvePrecedence(t *testing.T) {
	got := Resolve(
		domain.Appearance{Accent: "teal", Density: "compact", Textures: "off"},
		domain.Appearance{Accent: "cobalt", Swimlane: "group"},
	)
	assertEqual(t, got, ResolvedAppearance{
		Theme:    "system",
		Accent:   "cobalt",
		Density:  "compact",
		Swimlane: "group",
		Textures: "off",
		Source: map[string]string{
			"theme":    "default",
			"accent":   "project",
			"density":  "global",
			"swimlane": "project",
			"textures": "global",
		},
	})
}

func TestResolveDefaults(t *testing.T) {
	got := Resolve(domain.Appearance{}, domain.Appearance{})
	assertEqual(t, got, ResolvedAppearance{
		Theme:    "system",
		Accent:   "orange",
		Density:  "comfortable",
		Swimlane: "none",
		Textures: "on",
		Source: map[string]string{
			"theme":    "default",
			"accent":   "default",
			"density":  "default",
			"swimlane": "default",
			"textures": "default",
		},
	})
}

func TestResolveProjectBeatsGlobalOnEveryField(t *testing.T) {
	g := domain.Appearance{Theme: "light", Accent: "teal", Density: "comfortable", Swimlane: "none", Textures: "on"}
	p := domain.Appearance{Theme: "dark", Accent: "#7c3aed", Density: "compact", Swimlane: "priority", Textures: "off"}
	got := Resolve(g, p)
	assertEqual(t, got.Theme, "dark")
	assertEqual(t, got.Accent, "#7c3aed")
	assertEqual(t, got.Density, "compact")
	assertEqual(t, got.Swimlane, "priority")
	assertEqual(t, got.Textures, "off")
	for field, src := range got.Source {
		if src != "project" {
			t.Errorf("source[%s] = %q, want project", field, src)
		}
	}
}

func TestResolvedAppearanceJSON(t *testing.T) {
	raw, err := json.Marshal(Resolve(domain.Appearance{}, domain.Appearance{}))
	assertNoErr(t, err)
	var m map[string]any
	assertNoErr(t, json.Unmarshal(raw, &m))
	for _, k := range []string{"theme", "accent", "density", "swimlane", "textures", "source"} {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON lacks key %q: %s", k, raw)
		}
	}
	if len(m) != 6 {
		t.Errorf("unexpected keys in %s", raw)
	}
}

func TestTLSDefault(t *testing.T) {
	env := envOf(map[string]string{"HOME": "/h"})
	missing, err := Load(filepath.Join(t.TempDir(), "none.yaml"), env)
	assertNoErr(t, err)
	assertEqual(t, missing.TLS, "auto")

	path := filepath.Join(t.TempDir(), "c.yaml")
	assertNoErr(t, os.WriteFile(path, []byte("port: 9000\n"), 0o644))
	partial, err := Load(path, env)
	assertNoErr(t, err)
	assertEqual(t, partial.TLS, "auto")
}

func TestLoadTLSValues(t *testing.T) {
	env := envOf(map[string]string{"HOME": "/h"})
	for _, v := range []string{"auto", "on", "off"} {
		path := filepath.Join(t.TempDir(), "c.yaml")
		assertNoErr(t, os.WriteFile(path, []byte("tls: "+v+"\n"), 0o644))
		c, err := Load(path, env)
		assertNoErr(t, err)
		assertEqual(t, c.TLS, v)
	}
}

func TestLoadTLSInvalidNamesFile(t *testing.T) {
	env := envOf(map[string]string{"HOME": "/h"})
	for _, v := range []string{"yes", "true", "ON", `""`} {
		path := filepath.Join(t.TempDir(), "c.yaml")
		assertNoErr(t, os.WriteFile(path, []byte("tls: "+v+"\n"), 0o644))
		_, err := Load(path, env)
		if err == nil {
			t.Fatalf("tls: %s loaded without error", v)
		}
		if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "tls") {
			t.Errorf("tls: %s: error %q should name the file and the key", v, err)
		}
	}
}

func TestSaveLoadKeepsTLS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	env := envOf(map[string]string{"HOME": "/h"})
	assertNoErr(t, Update(path, env, func(c *Config) error { c.TLS = "off"; return nil }))
	c, err := Load(path, env)
	assertNoErr(t, err)
	assertEqual(t, c.TLS, "off")
}

func TestResolveSkipsAnInvalidValue(t *testing.T) {
	got := Resolve(
		domain.Appearance{Theme: "blue", Accent: "teal", Density: "tiny", Swimlane: "group"},
		domain.Appearance{Theme: "neon", Accent: "#xyz", Density: "compact", Textures: "furry"},
	)
	assertEqual(t, got, ResolvedAppearance{
		Theme:    "system",
		Accent:   "teal",
		Density:  "compact",
		Swimlane: "group",
		Textures: "on",
		Source: map[string]string{
			"theme": "default", "accent": "global", "density": "project", "swimlane": "global", "textures": "default",
		},
	})
}
