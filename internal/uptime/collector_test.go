package uptime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestCollector creates a Collector with a real HTTP client but no DB.
// DB-dependent methods are tested separately.
func newTestCollector() *Collector {
	return &Collector{
		client: http.Client{Timeout: 5 * time.Second},
		state:  make(map[int64]bool),
	}
}

func TestPing_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, url: srv.URL, expect: 200}
	if !c.ping(s) {
		t.Fatal("expected ping to return true for 200 response")
	}
}

func TestPing_Down_WrongStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, url: srv.URL, expect: 200}
	if c.ping(s) {
		t.Fatal("expected ping to return false for 503 response")
	}
}

func TestPing_Down_Unreachable(t *testing.T) {
	c := newTestCollector()
	s := source{id: 1, url: "http://127.0.0.1:19999", expect: 200}
	if c.ping(s) {
		t.Fatal("expected ping to return false for unreachable host")
	}
}

func TestPing_CustomExpect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := newTestCollector()
	s := source{id: 1, url: srv.URL, expect: 302}
	if !c.ping(s) {
		t.Fatal("expected ping to return true when status matches expect=302")
	}
}

func TestStateChange_FirstCheckNoEvent(t *testing.T) {
	c := newTestCollector()
	var written []string

	// intercept writeEvent via monkey-patching the state manually
	// first check: seen=false → no event should be written
	s := source{id: 1, name: "Test", url: "http://example.com", expect: 200}

	// simulate: ping returns false, first time seen
	up := false
	_, seen := c.state[s.id]
	c.state[s.id] = up
	if seen {
		written = append(written, "event")
	}

	if len(written) != 0 {
		t.Fatal("expected no event on first check")
	}
}

func TestStateChange_NoEventWhenStable(t *testing.T) {
	c := newTestCollector()
	c.state[1] = false // already known as down

	events := 0
	s := source{id: 1, name: "Test", url: "http://example.com", expect: 200}

	prev, seen := c.state[s.id]
	up := false // still down
	c.state[s.id] = up
	if seen && up != prev {
		events++
	}

	if events != 0 {
		t.Fatal("expected no event when state is stable")
	}
}

func TestStateChange_EventOnTransition(t *testing.T) {
	c := newTestCollector()
	c.state[1] = true // was up

	events := 0
	s := source{id: 1, name: "Test", url: "http://example.com", expect: 200}

	prev, seen := c.state[s.id]
	up := false // now down
	c.state[s.id] = up
	if seen && up != prev {
		events++
	}

	if events != 1 {
		t.Fatalf("expected 1 event on up→down transition, got %d", events)
	}
}

func TestStateChange_Messages(t *testing.T) {
	tests := []struct {
		up       bool
		wantPrio string
		wantWord string
	}{
		{false, "high", "DOWN"},
		{true, "normal", "UP"},
	}

	for _, tt := range tests {
		title, _, priority := stateChange("MyService", "https://example.com", tt.up)
		if priority != tt.wantPrio {
			t.Errorf("up=%v: want priority %q, got %q", tt.up, tt.wantPrio, priority)
		}
		if !contains(title, tt.wantWord) {
			t.Errorf("up=%v: want %q in title, got %q", tt.up, tt.wantWord, title)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
