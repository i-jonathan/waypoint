package httpapi

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func TestBrokerFanOut(t *testing.T) {
	b := NewBroker()
	a, cancelA := b.Subscribe()
	defer cancelA()
	c, cancelC := b.Subscribe()
	defer cancelC()

	want := Event{Type: "changed", Project: "Demo"}
	b.Publish(want)

	if got := receive(t, a); got != want {
		t.Fatalf("first subscriber got %#v, want %#v", got, want)
	}
	if got := receive(t, c); got != want {
		t.Fatalf("second subscriber got %#v, want %#v", got, want)
	}
}

func TestBrokerSlowSubscriberDoesNotBlock(t *testing.T) {
	b := NewBroker()
	_, cancel := b.Subscribe() // never read
	defer cancel()

	start := time.Now()
	for i := 0; i < 100; i++ {
		b.Publish(Event{Type: "changed", Project: "Demo"})
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("100 publishes took %v, want under 50ms", d)
	}
}

func TestBrokerSlowSubscriberKeepsFirstSixteen(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	defer cancel()
	for i := 0; i < 20; i++ {
		b.Publish(Event{Type: "changed", Project: string(rune('a' + i))})
	}
	n := 0
	for len(ch) > 0 {
		<-ch
		n++
	}
	if n != 16 {
		t.Fatalf("buffered %d events, want 16", n)
	}
}

func TestBrokerCancelStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	cancel()
	cancel() // idempotent
	b.Publish(Event{Type: "changed", Project: "Demo"})
	if e, ok := <-ch; ok {
		t.Fatalf("received %#v after cancel", e)
	}
	if n := b.subscriberCount(); n != 0 {
		t.Fatalf("%d subscribers after cancel, want 0", n)
	}
}

// openSSE starts a server around b and opens one stream. The response arrives
// only after the handler has subscribed, so a publish made afterwards is seen.
func openSSE(t *testing.T, b *Broker) (*bufio.Reader, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(b.ServeSSE))
	resp, err := http.Get(srv.URL)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q", got)
	}
	return bufio.NewReader(resp.Body), func() {
		resp.Body.Close()
		srv.Close()
	}
}

func readLines(t *testing.T, r *bufio.Reader, n int) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			line, err := r.ReadString('\n')
			sb.WriteString(line)
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("timed out reading the stream")
		return ""
	}
}

func TestSSEFormat(t *testing.T) {
	b := NewBroker()
	r, closeAll := openSSE(t, b)
	defer closeAll()

	b.Publish(Event{Type: "changed", Project: "Demo"})

	got := readLines(t, r, 3)
	want := "event: changed\ndata: {\"type\":\"changed\",\"project\":\"Demo\"}\n\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSSEPing(t *testing.T) {
	old := pingInterval
	pingInterval = 20 * time.Millisecond
	defer func() { pingInterval = old }()

	b := NewBroker()
	r, closeAll := openSSE(t, b)
	defer closeAll()

	if got := readLines(t, r, 2); got != ": ping\n\n" {
		t.Fatalf("got %q, want a ping comment", got)
	}
}

func TestSSEUnsubscribesWhenClientGoes(t *testing.T) {
	b := NewBroker()
	_, closeAll := openSSE(t, b)
	if n := b.subscriberCount(); n != 1 {
		t.Fatalf("%d subscribers while connected, want 1", n)
	}
	closeAll()

	deadline := time.Now().Add(3 * time.Second)
	for b.subscriberCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d subscribers leaked after the client left", b.subscriberCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSSEReturnsWhenRequestContextEnds(t *testing.T) {
	b := NewBroker()
	ctx, cancel := contextWithCancel(t)
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder() // a ResponseRecorder is a Flusher

	done := make(chan struct{})
	go func() {
		b.ServeSSE(rec, req)
		close(done)
	}()

	waitFor(t, func() bool { return b.subscriberCount() == 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeSSE did not return after the request context ended")
	}
	if n := b.subscriberCount(); n != 0 {
		t.Fatalf("%d subscribers after return, want 0", n)
	}
}

// plainWriter is a ResponseWriter that cannot flush.
type plainWriter struct {
	header http.Header
	code   int
}

func (p *plainWriter) Header() http.Header         { return p.header }
func (p *plainWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *plainWriter) WriteHeader(code int)        { p.code = code }

func TestSSEWithoutFlusherIs500(t *testing.T) {
	b := NewBroker()
	w := &plainWriter{header: http.Header{}}
	b.ServeSSE(w, httptest.NewRequest("GET", "/api/events", nil))
	if w.code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.code)
	}
	if n := b.subscriberCount(); n != 0 {
		t.Fatalf("%d subscribers, want 0", n)
	}
}

func TestSSESurvivesServerWriteTimeout(t *testing.T) {
	b := NewBroker()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(b.ServeSSE))
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)

	time.Sleep(300 * time.Millisecond) // well past the timeout
	b.Publish(Event{Type: "changed", Project: "Demo"})

	got := readLines(t, r, 3)
	want := "event: changed\ndata: {\"type\":\"changed\",\"project\":\"Demo\"}\n\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
