package cli

import (
	"errors"
	"path/filepath"
	"testing"

	"waypoint/internal/server"
)

func stubInstall(t *testing.T, fn func(server.Certs) error) {
	t.Helper()
	old := installCA
	installCA = fn
	t.Cleanup(func() { installCA = old })
}

func TestTrustCreatesTheCAThenInstallsIt(t *testing.T) {
	var got server.Certs
	stubInstall(t, func(c server.Certs) error {
		got = c
		mustExist(t, filepath.Join(c.Dir, "ca.pem")) // Ensure ran first
		return nil
	})
	stdout, stderr, code := runCLI(t, t.TempDir(), "trust")
	assertEq(t, code, 0)
	assertEq(t, stderr, "")
	assertEq(t, stdout, "Trusted the Waypoint local CA. Restart \"waypoint serve\" to use HTTPS and HTTP/2.\n")
	assertEq(t, got.Dir, filepath.Join(sandboxFor(t).home, ".local", "share", "waypoint", "tls"))
	assertEq(t, got.Home, sandboxFor(t).home)
}

func TestTrustHonoursDataDir(t *testing.T) {
	data := t.TempDir()
	writeConfig(t, "data_dir: "+data+"\n")
	stubInstall(t, func(server.Certs) error { return nil })
	_, _, code := runCLI(t, t.TempDir(), "trust")
	assertEq(t, code, 0)
	mustExist(t, filepath.Join(data, "tls", "ca.pem"))
}

func TestTrustReportsAnInstallFailure(t *testing.T) {
	stubInstall(t, func(server.Certs) error { return errors.New("run these yourself: sudo ...") })
	stdout, stderr, code := runCLI(t, t.TempDir(), "trust")
	assertEq(t, code, 1)
	assertEq(t, stdout, "")
	assertContains(t, stderr, "run these yourself")
}

func TestTrustRejectsArguments(t *testing.T) {
	stubInstall(t, func(server.Certs) error { t.Fatal("must not install"); return nil })
	_, _, code := runCLI(t, t.TempDir(), "trust", "now")
	assertEq(t, code, 2)
}
