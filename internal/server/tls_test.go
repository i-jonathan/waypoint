package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func init() {
	// No test may touch the machine's trust store: any command Install tries
	// to run without an explicit stub fails the test binary.
	runCommand = func(name string, args ...string) error {
		panic("unexpected command in a test: " + name + " " + strings.Join(args, " "))
	}
}

func newCerts(t *testing.T) Certs {
	t.Helper()
	return Certs{Dir: filepath.Join(t.TempDir(), "tls")}
}

func readPEMCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("%s: no PEM block", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEnsureCreatesAndReuses(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	ca1 := mustRead(t, filepath.Join(c.Dir, "ca.pem"))
	caKey1 := mustRead(t, filepath.Join(c.Dir, "ca.key"))
	leaf1 := mustRead(t, filepath.Join(c.Dir, "leaf.pem"))

	caCert := readPEMCert(t, filepath.Join(c.Dir, "ca.pem"))
	if !caCert.IsCA || caCert.Subject.CommonName != "Waypoint Local CA" {
		t.Fatalf("CA: IsCA=%v CN=%q", caCert.IsCA, caCert.Subject.CommonName)
	}
	if got := caCert.NotAfter.Sub(caCert.NotBefore); got < 9*365*24*time.Hour {
		t.Fatalf("CA lifetime %v, want about 10 years", got)
	}
	if caCert.PublicKeyAlgorithm != x509.ECDSA {
		t.Fatalf("CA key algorithm %v", caCert.PublicKeyAlgorithm)
	}

	leaf := readPEMCert(t, filepath.Join(c.Dir, "leaf.pem"))
	if got := leaf.NotAfter.Sub(t0); got < 396*24*time.Hour || got > 397*24*time.Hour {
		t.Fatalf("leaf valid for %v from now, want 397 days", got)
	}
	if !reflect.DeepEqual(leaf.DNSNames, []string{"localhost"}) || len(leaf.IPAddresses) != 2 ||
		leaf.IPAddresses[0].String() != "127.0.0.1" || leaf.IPAddresses[1].String() != "::1" {
		t.Fatalf("leaf SANs: %v %v", leaf.DNSNames, leaf.IPAddresses)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost", CurrentTime: t0.Add(time.Hour)}); err != nil {
		t.Fatalf("leaf does not verify against the CA: %v", err)
	}

	// A second call changes nothing.
	if _, err := c.Ensure(t0.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca1, mustRead(t, filepath.Join(c.Dir, "ca.pem"))) ||
		!bytes.Equal(caKey1, mustRead(t, filepath.Join(c.Dir, "ca.key"))) ||
		!bytes.Equal(leaf1, mustRead(t, filepath.Join(c.Dir, "leaf.pem"))) {
		t.Fatal("a second Ensure regenerated something")
	}

	// 380 days on, fewer than 30 days remain: new leaf, same CA.
	later := t0.Add(380 * 24 * time.Hour)
	if _, err := c.Ensure(later); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca1, mustRead(t, filepath.Join(c.Dir, "ca.pem"))) {
		t.Fatal("the CA changed")
	}
	leaf2 := mustRead(t, filepath.Join(c.Dir, "leaf.pem"))
	if bytes.Equal(leaf1, leaf2) {
		t.Fatal("the leaf was not re-issued")
	}
	newLeaf := readPEMCert(t, filepath.Join(c.Dir, "leaf.pem"))
	if _, err := newLeaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost", CurrentTime: later}); err != nil {
		t.Fatalf("re-issued leaf does not verify: %v", err)
	}
}

func TestEnsureReissuesLeafWithin30DaysOnlyWhenClose(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	leaf1 := mustRead(t, filepath.Join(c.Dir, "leaf.pem"))
	if _, err := c.Ensure(t0.Add(360 * 24 * time.Hour)); err != nil { // 37 days left
		t.Fatal(err)
	}
	if !bytes.Equal(leaf1, mustRead(t, filepath.Join(c.Dir, "leaf.pem"))) {
		t.Fatal("re-issued with more than 30 days left")
	}
}

func TestEnsureFileModes(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(c.Dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v err %v, want 0700", info.Mode().Perm(), err)
	}
	for _, f := range []string{"ca.pem", "ca.key", "leaf.pem", "leaf.key"} {
		info, err := os.Stat(filepath.Join(c.Dir, f))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v err %v, want 0600", f, info.Mode().Perm(), err)
		}
	}
	entries, _ := os.ReadDir(c.Dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestEnsureRefusesAReadableKey(t *testing.T) {
	for _, key := range []string{"ca.key", "leaf.key"} {
		c := newCerts(t)
		if _, err := c.Ensure(t0); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(c.Dir, key)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		before := map[string][]byte{}
		for _, f := range []string{"ca.pem", "ca.key", "leaf.pem", "leaf.key"} {
			before[f] = mustRead(t, filepath.Join(c.Dir, f))
		}

		// Even when the leaf is due for renewal, nothing is regenerated.
		_, err := c.Ensure(t0.Add(380 * 24 * time.Hour))
		if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "chmod 600") {
			t.Fatalf("%s: err = %v, want a clear error naming the file", key, err)
		}
		for f, want := range before {
			if !bytes.Equal(want, mustRead(t, filepath.Join(c.Dir, f))) {
				t.Fatalf("%s: %s was rewritten", key, f)
			}
		}
	}
}

func TestEnsureRefusesAHalfCA(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(c.Dir, "ca.key")); err != nil {
		t.Fatal(err)
	}
	ca := mustRead(t, filepath.Join(c.Dir, "ca.pem"))
	if _, err := c.Ensure(t0); err == nil || !strings.Contains(err.Error(), "ca.key") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(ca, mustRead(t, filepath.Join(c.Dir, "ca.pem"))) {
		t.Fatal("the CA was replaced")
	}
}

func TestEnsureReplacesAGarbageLeaf(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "leaf.pem"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	readPEMCert(t, filepath.Join(c.Dir, "leaf.pem"))
}

func TestTrustedIsAMarkerFile(t *testing.T) {
	c := newCerts(t)
	if c.Trusted() {
		t.Fatal("trusted before anything happened")
	}
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	if c.Trusted() {
		t.Fatal("Ensure must not mark the CA trusted")
	}
	if err := os.WriteFile(filepath.Join(c.Dir, "trusted"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.Trusted() {
		t.Fatal("marker file not noticed")
	}
}

func TestInstallDarwinRunsSecurityAndWritesMarker(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	var got [][]string
	defer swap(&goos, "darwin")()
	defer swap(&userHomeDir, func() (string, error) { return "/Users/someone", nil })()
	defer swap(&runCommand, func(name string, args ...string) error {
		got = append(got, append([]string{name}, args...))
		return nil
	})()

	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl",
		"-k", "/Users/someone/Library/Keychains/login.keychain-db", filepath.Join(c.Dir, "ca.pem")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	if !c.Trusted() {
		t.Fatal("no marker after a successful install")
	}
}

func TestInstallUsesTheHomeInCerts(t *testing.T) {
	c := newCerts(t)
	c.Home = "/elsewhere"
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	var got []string
	defer swap(&goos, "darwin")()
	defer swap(&runCommand, func(name string, args ...string) error {
		got = args
		return nil
	})()
	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	if got[6] != "/elsewhere/Library/Keychains/login.keychain-db" {
		t.Fatalf("keychain = %q", got[6])
	}
}

func TestInstallFailureWritesNoMarker(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	defer swap(&goos, "darwin")()
	defer swap(&userHomeDir, func() (string, error) { return "/h", nil })()
	defer swap(&runCommand, func(string, ...string) error { return os.ErrPermission })()
	if err := c.Install(); err == nil {
		t.Fatal("want an error")
	}
	if c.Trusted() {
		t.Fatal("marker written after a failed install")
	}
}

func TestInstallLinuxListsManualCommands(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	defer swap(&goos, "linux")()
	ran := false
	defer swap(&runCommand, func(string, ...string) error { ran = true; return nil })()
	err := c.Install()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, s := range []string{"update-ca-certificates", "certutil", filepath.Join(c.Dir, "ca.pem")} {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error lacks %q: %v", s, err)
		}
	}
	if ran || c.Trusted() {
		t.Fatal("linux install must run nothing and write no marker")
	}
}

func TestInstallWithoutACA(t *testing.T) {
	c := newCerts(t)
	defer swap(&goos, "darwin")()
	if err := c.Install(); err == nil {
		t.Fatal("want an error when ca.pem does not exist")
	}
}

// swap sets *p to v and returns a func that restores it.
func swap[T any](p *T, v T) func() {
	old := *p
	*p = v
	return func() { *p = old }
}

func caFiles(t *testing.T, c Certs) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	ca, err := parseCert(mustRead(t, c.path(caCertFile)))
	if err != nil {
		t.Fatal(err)
	}
	key, err := parseKey(mustRead(t, c.path(caKeyFile)))
	if err != nil {
		t.Fatal(err)
	}
	return ca, key
}

// leafFor issues what an attacker holding the CA's key could issue.
func leafFor(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dns []string, ips []net.IP) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    t0.Add(-time.Hour),
		NotAfter:     t0.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func verify(leaf, ca *x509.Certificate, name string) error {
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots, DNSName: name, CurrentTime: t0, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}

func TestCACannotVouchForAnythingButLocalhost(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	ca, key := caFiles(t, c)

	if !ca.PermittedDNSDomainsCritical {
		t.Fatal("the name constraints are not critical")
	}
	if err := verify(leafFor(t, ca, key, []string{"www.bank.example"}, nil), ca, "www.bank.example"); err == nil {
		t.Fatal("a leaf for www.bank.example verified")
	}
	if err := verify(leafFor(t, ca, key, nil, []net.IP{net.ParseIP("8.8.8.8")}), ca, "8.8.8.8"); err == nil {
		t.Fatal("a leaf for 8.8.8.8 verified")
	}

	leaf := readPEMCert(t, c.path(leafCert))
	for _, name := range []string{"localhost", "127.0.0.1", "::1"} {
		if err := verify(leaf, ca, name); err != nil {
			t.Fatalf("the server's own leaf for %s: %v", name, err)
		}
	}
}

// oldCA writes a CA as earlier versions made it, with no name constraints, and
// the marker that says it was trusted.
func oldCA(t *testing.T, c Certs) {
	t.Helper()
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: caCommonName},
		NotBefore: t0.Add(-time.Hour), NotAfter: t0.Add(caLifetime),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
		MaxPathLen: 0, MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{caKeyFile: keyPEM, caCertFile: encodeCert(der), trustedFile: nil} {
		if err := writeFileAtomic(c.path(name), data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnsureRegeneratesACAWithoutConstraints(t *testing.T) {
	c := newCerts(t)
	oldCA(t, c)
	before := mustRead(t, c.path(caKeyFile))

	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, mustRead(t, c.path(caKeyFile))) {
		t.Fatal("the unconstrained CA's key is still in use")
	}
	ca, _ := caFiles(t, c)
	if !ca.PermittedDNSDomainsCritical {
		t.Fatal("the new CA has no constraints")
	}
	if c.Trusted() {
		t.Fatal("the trusted marker survived a new CA")
	}
	if err := verify(readPEMCert(t, c.path(leafCert)), ca, "localhost"); err != nil {
		t.Fatalf("the leaf was not reissued: %v", err)
	}
}

func TestEnsureKeepsAConstrainedCA(t *testing.T) {
	c := newCerts(t)
	if _, err := c.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, c.path(caKeyFile))
	if _, err := c.Ensure(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, mustRead(t, c.path(caKeyFile))) {
		t.Fatal("a good CA was replaced")
	}
}
