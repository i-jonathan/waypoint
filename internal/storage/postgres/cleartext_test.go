package postgres_test

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"waypoint/internal/config"
	"waypoint/internal/dbconn"
	"waypoint/internal/storage/postgres"
)

// A server that asks for the password in clear must not get it. Nothing here
// needs a real Postgres: the fake server only plays the start of the handshake.
func TestNeverSendsTheClearTextPassword(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mu sync.Mutex
	var got []byte
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for {
					head := make([]byte, 8)
					if _, err := io.ReadFull(c, head); err != nil {
						return
					}
					n := binary.BigEndian.Uint32(head[:4])
					rest := make([]byte, n-8)
					io.ReadFull(c, rest)
					if binary.BigEndian.Uint32(head[4:]) == 80877103 { // SSLRequest: no TLS here
						c.Write([]byte{'N'})
						continue
					}
					c.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 3}) // AuthenticationCleartextPassword
					buf := make([]byte, 256)
					k, _ := c.Read(buf)
					mu.Lock()
					got = append(got, buf[:k]...)
					mu.Unlock()
					return
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	ref := config.ProjectRef{Name: "evil", Storage: "postgres", Host: "127.0.0.1", Port: addr.Port, User: "u", Database: "d", SSL: "disable"}
	t.Setenv(dbconn.PasswordVar("evil"), "MARKERpw")

	_, err = postgres.Probe(ctx, ref)
	if err == nil {
		t.Fatal("a cleartext-password server should be refused")
	}
	if errors.Is(err, dbconn.ErrUnreachable) || !strings.Contains(err.Error(), "SCRAM") {
		t.Fatalf("the refusal should say Waypoint accepts only SCRAM, not that the server is unreachable: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(string(got), "MARKERpw") {
		t.Fatalf("the server received the password: %q", got)
	}
	if strings.Contains(err.Error(), "MARKERpw") {
		t.Fatalf("the error holds the password: %v", err)
	}
}
