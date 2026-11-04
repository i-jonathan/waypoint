// Package server runs Waypoint's HTTP server and manages the local certificate
// authority that lets browsers reach it over HTTPS (and so HTTP/2).
package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	caCertFile  = "ca.pem"
	caKeyFile   = "ca.key"
	leafCert    = "leaf.pem"
	leafKey     = "leaf.key"
	trustedFile = "trusted"

	caCommonName = "Waypoint Local CA"
	caLifetime   = 10 * 365 * 24 * time.Hour
	// leafDays stays under the 398 days browsers accept for a server certificate.
	leafDays      = 397
	leafRenewWhen = 30 * 24 * time.Hour
)

// Certs is the local certificate authority and the certificate it issues for
// the server. Dir is data_dir/tls.
type Certs struct {
	Dir  string
	Home string // whose login keychain Install uses; empty means the user's home directory
}

// Test seams. Nothing outside tests changes them.
var (
	goos        = runtime.GOOS
	userHomeDir = os.UserHomeDir
	// runCommand runs an external command. Install is the only caller; it
	// changes the machine's trust store, so tests replace this.
	runCommand = func(name string, args ...string) error {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

func (c Certs) path(name string) string { return filepath.Join(c.Dir, name) }

// Trusted reports whether Install has succeeded: the marker file exists.
func (c Certs) Trusted() bool {
	_, err := os.Stat(c.path(trustedFile))
	return err == nil
}

// Ensure returns the server certificate, creating what is missing. The CA
// (ECDSA P-256, 10 years) is made once and never replaced. The leaf, valid for
// localhost, 127.0.0.1 and ::1, is issued for 397 days and re-issued when fewer
// than 30 remain. All files are 0600 in a 0700 directory. A key file that other
// users can read is an error and nothing is rewritten.
func (c Certs) Ensure(now time.Time) (tls.Certificate, error) {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("create %s: %w", c.Dir, err)
	}
	if err := os.Chmod(c.Dir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("secure %s: %w", c.Dir, err)
	}
	lock := flock.New(c.path(".lock"))
	if err := lock.Lock(); err != nil {
		return tls.Certificate{}, fmt.Errorf("lock %s: %w", c.Dir, err)
	}
	defer lock.Unlock()

	for _, k := range []string{caKeyFile, leafKey} {
		if err := c.checkKeyMode(k); err != nil {
			return tls.Certificate{}, err
		}
	}
	ca, caKey, err := c.loadOrCreateCA(now)
	if err != nil {
		return tls.Certificate{}, err
	}
	return c.loadOrIssueLeaf(now, ca, caKey)
}

// checkKeyMode refuses a key file that is not a plain file private to its owner.
func (c Certs) checkKeyMode(name string) error {
	info, err := os.Lstat(c.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", c.path(name))
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s can be read by other users (mode %04o); run: chmod 600 %s", c.path(name), info.Mode().Perm(), c.path(name))
	}
	return nil
}

func (c Certs) loadOrCreateCA(now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certData, certErr := os.ReadFile(c.path(caCertFile))
	keyData, keyErr := os.ReadFile(c.path(caKeyFile))
	switch {
	case errors.Is(certErr, fs.ErrNotExist) && errors.Is(keyErr, fs.ErrNotExist):
		return c.createCA(now)
	case errors.Is(certErr, fs.ErrNotExist):
		return nil, nil, fmt.Errorf("%s is missing but %s exists; restore it, or delete %s to start over", c.path(caCertFile), caKeyFile, c.Dir)
	case errors.Is(keyErr, fs.ErrNotExist):
		return nil, nil, fmt.Errorf("%s is missing but %s exists; restore it, or delete %s to start over", c.path(caKeyFile), caCertFile, c.Dir)
	case certErr != nil:
		return nil, nil, certErr
	case keyErr != nil:
		return nil, nil, keyErr
	}
	ca, err := parseCert(certData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w; delete %s to start over", c.path(caCertFile), err, c.Dir)
	}
	key, err := parseKey(keyData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w; delete %s to start over", c.path(caKeyFile), err, c.Dir)
	}
	pub, ok := ca.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) || !ca.IsCA {
		return nil, nil, fmt.Errorf("%s and %s do not belong together; delete %s to start over", caCertFile, caKeyFile, c.Dir)
	}
	if now.After(ca.NotAfter) {
		return nil, nil, fmt.Errorf("the Waypoint local CA expired on %s; delete %s and run waypoint trust again", ca.NotAfter.Format("2006-01-02"), c.Dir)
	}
	return ca, key, nil
}

func (c Certs) createCA(now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caLifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	// A new CA invalidates whatever trust was recorded for an old one.
	if err := os.Remove(c.path(trustedFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	// The key goes first: a certificate without its key is the worse half to be left with.
	if err := writeFileAtomic(c.path(caKeyFile), keyPEM); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(c.path(caCertFile), encodeCert(der)); err != nil {
		return nil, nil, err
	}
	return ca, key, nil
}

func (c Certs) loadOrIssueLeaf(now time.Time, ca *x509.Certificate, caKey *ecdsa.PrivateKey) (tls.Certificate, error) {
	if cert, ok := c.currentLeaf(now, ca); ok {
		return cert, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := newSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(leafDays * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := encodeCert(der)
	if err := writeFileAtomic(c.path(leafKey), keyPEM); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeFileAtomic(c.path(leafCert), certPEM); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// currentLeaf returns the stored leaf when it is usable: it parses, matches its
// key, was issued by ca, is valid now and has at least 30 days left.
func (c Certs) currentLeaf(now time.Time, ca *x509.Certificate) (tls.Certificate, bool) {
	certPEM, err1 := os.ReadFile(c.path(leafCert))
	keyPEM, err2 := os.ReadFile(c.path(leafKey))
	if err1 != nil || err2 != nil {
		return tls.Certificate{}, false
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, false
	}
	leaf, err := parseCert(certPEM)
	if err != nil || leaf.CheckSignatureFrom(ca) != nil {
		return tls.Certificate{}, false
	}
	if now.Before(leaf.NotBefore) || leaf.NotAfter.Sub(now) < leafRenewWhen {
		return tls.Certificate{}, false
	}
	return pair, true
}

// Install trusts the CA for the current user. On macOS it adds the CA to the
// login keychain; elsewhere it returns the commands to run by hand. The marker
// file Trusted looks for is written only after the CA was added.
func (c Certs) Install() error {
	ca := c.path(caCertFile)
	if _, err := os.Stat(ca); err != nil {
		return fmt.Errorf("no CA at %s: %w", ca, err)
	}
	switch goos {
	case "darwin":
		home := c.Home
		if home == "" {
			var err error
			if home, err = userHomeDir(); err != nil {
				return fmt.Errorf("cannot find the home directory: %w", err)
			}
		}
		keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
		if err := runCommand("security", "add-trusted-cert", "-r", "trustRoot", "-k", keychain, ca); err != nil {
			return fmt.Errorf("trust the CA: %w", err)
		}
	case "linux":
		return fmt.Errorf(`cannot trust the CA automatically on linux; run these yourself:
  sudo cp %[1]s /usr/local/share/ca-certificates/waypoint-local-ca.crt
  sudo update-ca-certificates
  certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n "Waypoint Local CA" -i %[1]s   (Chrome and Firefox, NSS)
then record it with: touch %[2]s`, ca, c.path(trustedFile))
	default:
		return fmt.Errorf("cannot trust the CA automatically on %s; trust %s in your system or browser by hand", goos, ca)
	}
	return writeFileAtomic(c.path(trustedFile), nil)
}

func newSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func encodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func parseCert(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("not a PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("not a PEM private key")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an ECDSA key")
	}
	return key, nil
}

// writeFileAtomic writes data to path with mode 0600 through a temp file in the
// same directory, so a reader never sees half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := bytes.NewReader(data).WriteTo(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
