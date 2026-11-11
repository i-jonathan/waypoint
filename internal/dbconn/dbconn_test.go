package dbconn

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestPasswordVar(t *testing.T) {
	cases := map[string]string{
		"pharmacy": "WAYPOINT_DB_PASSWORD_PHARMACY",
		"my-proj":  "WAYPOINT_DB_PASSWORD_MY_PROJ",
		"日本語":      "WAYPOINT_DB_PASSWORD____",
	}
	for in, want := range cases {
		if got := PasswordVar(in); got != want {
			t.Errorf("PasswordVar(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPassword(t *testing.T) {
	both := env(map[string]string{GlobalPasswordVar: "g", "WAYPOINT_DB_PASSWORD_A": "a"})
	if got := Password(both, "a"); got != "a" {
		t.Errorf("project var should win, got %q", got)
	}
	if got := Password(both, "b"); got != "g" {
		t.Errorf("another project should use the global, got %q", got)
	}
	empty := env(map[string]string{GlobalPasswordVar: "g", "WAYPOINT_DB_PASSWORD_A": ""})
	if got := Password(empty, "a"); got != "g" {
		t.Errorf("empty project var should fall back to global, got %q", got)
	}
	if got := Password(env(nil), "a"); got != "" {
		t.Errorf("unset should give empty, got %q", got)
	}
}

func TestPasswordHelpNamesBoth(t *testing.T) {
	h := PasswordHelp("pharmacy")
	want := "set WAYPOINT_DB_PASSWORD_PHARMACY for this project, or WAYPOINT_DB_PASSWORD for all of them"
	if h != want {
		t.Errorf("got %q, want %q", h, want)
	}
}

func TestIsLocal(t *testing.T) {
	for _, h := range []string{"localhost", "LOCALHOST", "127.0.0.1", "127.5.5.5", "::1", "[::1]", "/var/run/postgresql"} {
		if !IsLocal(h) {
			t.Errorf("%q should be local", h)
		}
	}
	for _, h := range []string{"db.example.com", "192.168.1.5", "10.0.0.2", "", "localhost.example.com"} {
		if IsLocal(h) {
			t.Errorf("%q should be non-local", h)
		}
	}
}

func TestDefaults(t *testing.T) {
	if DefaultPort("postgres") != 5432 {
		t.Error("default ports wrong")
	}
	if DefaultSSL("localhost") != "prefer" || DefaultSSL("db.example.com") != "require" {
		t.Error("default ssl wrong")
	}
	for _, v := range []string{"disable", "prefer", "require", "verify-full"} {
		if !ValidSSL(v) {
			t.Errorf("%q should be valid", v)
		}
	}
	if ValidSSL("bogus") || ValidSSL("") {
		t.Error("bogus and empty are not valid values")
	}
}

func TestScrubKeepsWrapping(t *testing.T) {
	secret := "p@ss:w/rd"
	err := Scrub(fmt.Errorf("dial failed using %s: %w", secret, io.EOF), secret)
	if strings.Contains(err.Error(), secret) {
		t.Errorf("secret still in %q", err)
	}
	if !errors.Is(err, io.EOF) {
		t.Error("wrapping lost")
	}
}

func TestScrubURLPassword(t *testing.T) {
	err := Scrub(errors.New("cannot connect to postgres://u:secret@h/db"))
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("url password still in %q", err)
	}
	if Scrub(nil) != nil {
		t.Error("nil should stay nil")
	}
}

func TestWarningText(t *testing.T) {
	if !strings.Contains(NonLocalWarning, "one person") {
		t.Errorf("warning should say it is for one person: %q", NonLocalWarning)
	}
}

func TestBareHost(t *testing.T) {
	for in, want := range map[string]string{"[::1]": "::1", "::1": "::1", "localhost": "localhost", "[fe80::1]": "fe80::1"} {
		if got := BareHost(in); got != want {
			t.Errorf("BareHost(%q) = %q, want %q", in, got, want)
		}
	}
}
