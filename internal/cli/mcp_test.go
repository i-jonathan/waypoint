package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// TestMCPSessionOverPipes drives the real "waypoint mcp" command path:
// initialize, then tools/list, over pipes. Stdout must hold nothing but
// protocol messages.
func TestMCPSessionOverPipes(t *testing.T) {
	dir := newProject(t)
	env, _, errb := testEnv(t, dir, nil)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	env.Stdin, env.Stdout = inR, outW

	done := make(chan int, 1)
	go func() {
		code := Run(context.Background(), []string{"mcp"}, env)
		outW.Close()
		done <- code
	}()

	send := func(s string) {
		if _, err := io.WriteString(inW, s+"\n"); err != nil {
			t.Error(err)
		}
	}
	rd := bufio.NewReader(outR)
	read := func() map[string]any {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatalf("reading a reply: %v (stderr %q)", err, errb.String())
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %q", line)
		}
		return m
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	if m := read(); m["id"] != float64(1) || m["result"] == nil {
		t.Fatalf("initialize reply = %v", m)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	m := read()
	tools := m["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("want 7 tools, got %d", len(tools))
	}

	inW.Close()
	select {
	case code := <-done:
		assertEq(t, code, 0)
	case <-time.After(10 * time.Second):
		t.Fatal("waypoint mcp did not exit when stdin closed")
	}
	if rest, _ := io.ReadAll(outR); len(rest) != 0 {
		t.Fatalf("extra stdout after the session: %q", rest)
	}
}

func TestMCPNeedsAProject(t *testing.T) {
	stdout, stderr, code := runCLI(t, t.TempDir(), "mcp")
	assertEq(t, code, 1)
	assertEq(t, stdout, "")
	assertContains(t, stderr, "no project here")
}

func TestMCPRejectsArguments(t *testing.T) {
	_, stderr, code := runCLI(t, t.TempDir(), "mcp", "extra")
	assertEq(t, code, 2)
	assertContains(t, stderr, "usage: waypoint mcp")
}
