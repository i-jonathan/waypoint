package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// TrustTip is printed once when tls is "auto" and the CA is not trusted yet.
const TrustTip = `Tip: run "waypoint trust" once to serve over HTTPS with HTTP/2.`

const shutdownTimeout = 5 * time.Second

// Options configures Run.
type Options struct {
	Port     int          // used when Listener is nil; 0 picks a free port
	Listener net.Listener // optional; a listener from Listen. Run closes it
	TLS      string       // "auto", "on" or "off"
	Certs    Certs
	Handler  http.Handler
	Now      func() time.Time     // nil means time.Now; for certificate dates
	Ready    func(url string)     // called once the server is about to accept, with "https://localhost:PORT/" or the http form
	Tip      func(msg string)     // called once with TrustTip when tls is auto and the CA is not trusted
	Broker   interface{ Close() } // closed before the graceful shutdown so open event streams end
}

// PortInUseError is returned when the port is taken.
type PortInUseError struct{ Port int }

func (e *PortInUseError) Error() string {
	return fmt.Sprintf(`port %d is in use; set "port" in CONFIG or pass --port`, e.Port)
}

// Listen binds 127.0.0.1:port. Waypoint never listens on another address. Bind
// first, then build the handler from the listener's real port: the Host check
// compares against it, and port 0 only has a real port after the bind.
func Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if errors.Is(err, syscall.EADDRINUSE) {
		return nil, &PortInUseError{Port: port}
	}
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", port, err)
	}
	return ln, nil
}

// newServer builds the http.Server: HTTP/1.1, HTTP/2 and h2c all enabled. There
// is no WriteTimeout, because event streams stay open for as long as a tab does.
func newServer(o Options) *http.Server {
	var p http.Protocols
	p.SetHTTP1(true)
	p.SetHTTP2(true)
	p.SetUnencryptedHTTP2(true)
	return &http.Server{
		Handler:           o.Handler,
		Protocols:         &p,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// Run serves o.Handler until ctx ends, then shuts down gracefully (5s).
// It serves HTTPS when TLS is "on", or "auto" with a trusted CA; otherwise HTTP.
func Run(ctx context.Context, o Options) error {
	ln := o.Listener
	if ln == nil {
		var err error
		if ln, err = Listen(o.Port); err != nil {
			return err
		}
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srv := newServer(o)
	useTLS := false
	switch o.TLS {
	case "on":
		useTLS = true
	case "off":
	case "auto":
		if useTLS = o.Certs.Trusted(); !useTLS && o.Tip != nil {
			o.Tip(TrustTip)
		}
	default:
		ln.Close()
		return fmt.Errorf("tls %q must be one of auto, on, off", o.TLS)
	}
	if useTLS {
		now := time.Now
		if o.Now != nil {
			now = o.Now
		}
		cert, err := o.Certs.Ensure(now())
		if err != nil {
			ln.Close()
			return err
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		// Ensure can replace an old CA, and then nothing trusts the new one yet.
		if o.TLS == "auto" && !o.Certs.Trusted() {
			useTLS = false
			if o.Tip != nil {
				o.Tip(TrustTip)
			}
		}
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}

	served := make(chan error, 1)
	go func() {
		if useTLS {
			served <- srv.ServeTLS(ln, "", "")
		} else {
			served <- srv.Serve(ln)
		}
	}()
	if o.Ready != nil {
		o.Ready(fmt.Sprintf("%s://localhost:%d/", scheme, port))
	}

	select {
	case err := <-served:
		return err // the listener failed; Shutdown was never asked for
	case <-ctx.Done():
	}

	// Open event streams would hold Shutdown to its timeout, so end them first.
	if o.Broker != nil {
		o.Broker.Close()
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := srv.Shutdown(sctx)
	if err != nil {
		srv.Close()
	}
	<-served
	if errors.Is(err, context.DeadlineExceeded) {
		return nil // forced closed after the grace period; the caller asked for the stop
	}
	return err
}
