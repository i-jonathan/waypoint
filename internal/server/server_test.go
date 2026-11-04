package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	httpapi "waypoint/internal/http"
)

// started is a running server for a test.
type started struct {
	url  string // as given to Ready
	port int
	done chan error
	stop context.CancelFunc

	once   sync.Once
	result error
}

// wait returns Run's result, waiting for it the first time only.
func (s *started) wait(t *testing.T) error {
	t.Helper()
	s.once.Do(func() {
		select {
		case s.result = <-s.done:
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	})
	return s.result
}

func start(t *testing.T, o Options) *started {
	t.Helper()
	if o.Handler == nil {
		o.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "proto=%s", r.Proto)
		})
	}
	if o.Certs.Dir == "" {
		o.Certs = newCerts(t)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &started{done: make(chan error, 1), stop: cancel}
	ready := make(chan string, 1)
	o.Ready = func(url string) { ready <- url }
	go func() { s.done <- Run(ctx, o) }()
	select {
	case s.url = <-ready:
	case err := <-s.done:
		t.Fatalf("Run returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}
	u := strings.TrimSuffix(strings.SplitN(s.url, "://", 2)[1], "/")
	_, p, _ := net.SplitHostPort(u)
	fmt.Sscan(p, &s.port)
	t.Cleanup(func() {
		cancel()
		s.wait(t)
	})
	return s
}

func (s *started) shutdown(t *testing.T) error {
	t.Helper()
	s.stop()
	return s.wait(t)
}

func caPool(t *testing.T, c Certs) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(mustRead(t, filepath.Join(c.Dir, "ca.pem"))) {
		t.Fatal("bad CA PEM")
	}
	return pool
}

func get(t *testing.T, c *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestServeH2OverTLS(t *testing.T) {
	certs := newCerts(t)
	s := start(t, Options{Port: 0, TLS: "on", Certs: certs})
	if !strings.HasPrefix(s.url, "https://localhost:") || !strings.HasSuffix(s.url, "/") {
		t.Fatalf("Ready url = %q", s.url)
	}
	client := &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{RootCAs: caPool(t, certs)},
	}}
	for _, host := range []string{"127.0.0.1", "localhost"} {
		resp := get(t, client, fmt.Sprintf("https://%s:%d/", host, s.port))
		if resp.ProtoMajor != 2 {
			t.Fatalf("%s: ProtoMajor = %d, want 2", host, resp.ProtoMajor)
		}
	}
}

func TestServeTLSOnWithoutTrustStillServesHTTPS(t *testing.T) {
	certs := newCerts(t) // not trusted
	var tips []string
	s := start(t, Options{TLS: "on", Certs: certs, Tip: func(m string) { tips = append(tips, m) }})
	if !strings.HasPrefix(s.url, "https://") || len(tips) != 0 {
		t.Fatalf("url %q tips %q", s.url, tips)
	}
}

func plainClient(protocols func(*http.Protocols)) *http.Client {
	var p http.Protocols
	protocols(&p)
	return &http.Client{Transport: &http.Transport{Protocols: &p}}
}

func TestServeH2C(t *testing.T) {
	s := start(t, Options{TLS: "off"})
	if !strings.HasPrefix(s.url, "http://localhost:") {
		t.Fatalf("Ready url = %q", s.url)
	}
	c := plainClient(func(p *http.Protocols) { p.SetUnencryptedHTTP2(true) })
	resp := get(t, c, fmt.Sprintf("http://127.0.0.1:%d/", s.port))
	if resp.ProtoMajor != 2 {
		t.Fatalf("ProtoMajor = %d, want 2", resp.ProtoMajor)
	}
}

func TestServeHTTP1Fallback(t *testing.T) {
	s := start(t, Options{TLS: "off"})
	c := plainClient(func(p *http.Protocols) { p.SetHTTP1(true) })
	resp := get(t, c, fmt.Sprintf("http://127.0.0.1:%d/", s.port))
	if resp.ProtoMajor != 1 {
		t.Fatalf("ProtoMajor = %d, want 1", resp.ProtoMajor)
	}
}

func TestAutoUntrustedServesHTTP(t *testing.T) {
	certs := newCerts(t)
	var tips []string
	s := start(t, Options{TLS: "auto", Certs: certs, Tip: func(m string) { tips = append(tips, m) }})
	if !strings.HasPrefix(s.url, "http://localhost:") {
		t.Fatalf("Ready url = %q", s.url)
	}
	resp := get(t, plainClient(func(p *http.Protocols) { p.SetHTTP1(true) }), fmt.Sprintf("http://127.0.0.1:%d/", s.port))
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	want := `Tip: run "waypoint trust" once to serve over HTTPS with HTTP/2.`
	if len(tips) != 1 || tips[0] != want {
		t.Fatalf("tips = %q, want exactly %q", tips, want)
	}
	if _, err := os.Stat(certs.Dir); err == nil {
		t.Fatal("certificates were generated although plain HTTP was served")
	}
}

func TestAutoTrustedServesHTTPS(t *testing.T) {
	certs := newCerts(t)
	if _, err := certs.Ensure(t0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certs.Dir, "trusted"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var tips []string
	s := start(t, Options{TLS: "auto", Certs: certs, Now: func() time.Time { return time.Now() }, Tip: func(m string) { tips = append(tips, m) }})
	if !strings.HasPrefix(s.url, "https://") || len(tips) != 0 {
		t.Fatalf("url %q tips %q", s.url, tips)
	}
	client := &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: caPool(t, certs)}}}
	if resp := get(t, client, fmt.Sprintf("https://localhost:%d/", s.port)); resp.ProtoMajor != 2 {
		t.Fatalf("ProtoMajor = %d", resp.ProtoMajor)
	}
}

func TestUnknownTLSModeIsAnError(t *testing.T) {
	err := Run(context.Background(), Options{TLS: "sometimes", Certs: newCerts(t), Handler: http.NotFoundHandler()})
	if err == nil || !strings.Contains(err.Error(), "sometimes") {
		t.Fatalf("err = %v", err)
	}
}

func TestPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	err = Run(context.Background(), Options{Port: port, TLS: "off", Handler: http.NotFoundHandler()})
	var inUse *PortInUseError
	if !errors.As(err, &inUse) || inUse.Port != port {
		t.Fatalf("err = %v, want a *PortInUseError for %d", err, port)
	}
	want := fmt.Sprintf(`port %d is in use; set "port" in CONFIG or pass --port`, port)
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func TestPortZeroReportsTheRealPort(t *testing.T) {
	s := start(t, Options{Port: 0, TLS: "off"})
	if s.port == 0 {
		t.Fatalf("Ready url %q still names port 0", s.url)
	}
}

func TestRunUsesAGivenListener(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	s := start(t, Options{Listener: ln, TLS: "off"})
	if s.port != port {
		t.Fatalf("served on %d, listener is %d", s.port, port)
	}
}

func TestListensOnLoopbackOnly(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ip := ln.Addr().(*net.TCPAddr).IP; !ip.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("bound to %v", ip)
	}
}

func TestServerTimeouts(t *testing.T) {
	srv := newServer(Options{Handler: http.NotFoundHandler()})
	if srv.ReadHeaderTimeout != 5*time.Second || srv.IdleTimeout != 120*time.Second || srv.WriteTimeout != 0 || srv.ReadTimeout != 0 {
		t.Fatalf("timeouts: header %v idle %v write %v read %v", srv.ReadHeaderTimeout, srv.IdleTimeout, srv.WriteTimeout, srv.ReadTimeout)
	}
	if !srv.Protocols.HTTP1() || !srv.Protocols.HTTP2() || !srv.Protocols.UnencryptedHTTP2() {
		t.Fatalf("protocols: %v", srv.Protocols)
	}
}

func TestShutdownDoesNotWaitForOpenEventStreams(t *testing.T) {
	broker := httpapi.NewBroker()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", broker.ServeSSE)
	s := start(t, Options{TLS: "off", Handler: mux, Broker: broker})

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/events", s.port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}

	began := time.Now()
	if err := s.shutdown(t); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("shutdown took %v with a tab open", took)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Logf("stream ended with %v", err) // either a clean end or a reset is fine
	}
}

func TestShutdownIsGracefulForInFlightRequests(t *testing.T) {
	var once sync.Once
	entered := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, "finished")
	})
	s := start(t, Options{TLS: "off", Handler: h})
	got := make(chan string, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", s.port))
		if err != nil {
			got <- err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		got <- string(b)
	}()
	<-entered
	if err := s.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if v := <-got; v != "finished" {
		t.Fatalf("in-flight request got %q", v)
	}
}
