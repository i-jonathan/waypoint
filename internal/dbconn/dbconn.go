// Package dbconn holds the rules for connecting to a database server that do
// not depend on the engine: where the password comes from, which servers count
// as local, TLS defaults and removing secrets from error messages. It imports
// only the standard library.
package dbconn

import (
	"net"
	"regexp"
	"strings"
)

// GlobalPasswordVar serves every server project that has no variable of its own.
const GlobalPasswordVar = "WAYPOINT_DB_PASSWORD"

// NonLocalWarning is shown once when a project is set up on a server that is
// not on this machine.
const NonLocalWarning = "This server is not on this computer. Waypoint is designed for one person's data, not for sharing a board between people. Connect only to a server you control."

// PasswordVar is the environment variable that holds the password of one
// project: its name upper-cased, every character other than A-Z and 0-9 turned
// into an underscore.
func PasswordVar(project string) string {
	var b strings.Builder
	b.WriteString(GlobalPasswordVar + "_")
	for _, r := range strings.ToUpper(project) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Password returns the project's password: its own variable if set and not
// empty, else the global one, else "".
func Password(getenv func(string) string, project string) string {
	if v := getenv(PasswordVar(project)); v != "" {
		return v
	}
	return getenv(GlobalPasswordVar)
}

// PasswordHelp is the sentence every message that asks for a password uses.
func PasswordHelp(project string) string {
	return "set " + PasswordVar(project) + " for this project, or " + GlobalPasswordVar + " for all of them"
}

// IsLocal reports whether host is this machine: localhost, 127.0.0.0/8, ::1
// or a Unix socket path.
func IsLocal(host string) bool {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.HasPrefix(host, "/") {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DefaultPort is the standard port of an engine ("postgres" or "mysql").
func DefaultPort(engine string) int {
	if engine == "mysql" {
		return 3306
	}
	return 5432
}

var sslModes = []string{"disable", "prefer", "require", "verify-full"}

// ValidSSL reports whether v is one of the accepted --ssl values.
func ValidSSL(v string) bool {
	for _, m := range sslModes {
		if v == m {
			return true
		}
	}
	return false
}

// DefaultSSL is "prefer" for a local server and "require" (encrypted, no
// certificate check) for any other.
func DefaultSSL(host string) string {
	if IsLocal(host) {
		return "prefer"
	}
	return "require"
}

var urlPassword = regexp.MustCompile(`(://[^/\s:@]*):[^@\s]*@`)

type scrubbed struct {
	msg string
	err error
}

func (e *scrubbed) Error() string { return e.msg }
func (e *scrubbed) Unwrap() error { return e.err }

// Scrub returns err with every secret, and any password in a user:pass@ URL,
// replaced in its message. errors.Is and errors.As still see the original.
func Scrub(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	out := msg
	for _, s := range secrets {
		if s != "" {
			out = strings.ReplaceAll(out, s, "***")
		}
	}
	out = urlPassword.ReplaceAllString(out, "$1:***@")
	if out == msg {
		return err
	}
	return &scrubbed{msg: out, err: err}
}
