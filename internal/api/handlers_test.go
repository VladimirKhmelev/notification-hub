package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// setupHandler creates a Handler with a real DB connection.
// Skips the test if DATABASE_URL is not set.
func setupHandler(t *testing.T) *Handler {
	t.Helper()
	db, err := sql.Open("postgres", "postgres://hub:hub@localhost:5432/hub?sslmode=disable")
	if err != nil {
		t.Skipf("db open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("db not available: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewHandler(db)
}

// createTestSource inserts a source and registers cleanup.
func createTestSource(t *testing.T, h *Handler, srcType, name string) Source {
	t.Helper()
	body := `{"type":"` + srcType + `","name":"` + name + `","config":{"url":"https://example.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/sources", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.createSource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestSource: expected 201, got %d", w.Code)
	}
	var s Source
	json.NewDecoder(w.Body).Decode(&s)
	t.Cleanup(func() { h.db.Exec(`DELETE FROM sources WHERE id = $1`, s.ID) })
	return s
}

// ── sources ───────────────────────────────────────────────────────────────────

func TestCreateSource_OK(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "uptime", "Test Site")
	if s.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if s.Name != "Test Site" {
		t.Fatalf("expected name %q, got %q", "Test Site", s.Name)
	}
	if s.MutedUntil != nil {
		t.Fatal("expected muted_until to be nil on create")
	}
}

func TestCreateSource_MissingName(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/sources", bytes.NewBufferString(`{"type":"uptime"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.createSource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSource_MissingType(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/sources", bytes.NewBufferString(`{"name":"X"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.createSource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateSource_InvalidJSON(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/sources", bytes.NewBufferString(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.createSource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListSources_OK(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	w := httptest.NewRecorder()
	h.listSources(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var sources []Source
	if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestListSources_ContainsMutedUntil(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteTest")

	// mute it
	muteReq := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"1h"}`))
	muteReq.URL.Path = "/sources/" + itoa(s.ID) + "/mute"
	muteReq.Header.Set("Content-Type", "application/json")
	mw := httptest.NewRecorder()
	h.muteSource(mw, muteReq, s.ID)
	if mw.Code != http.StatusNoContent {
		t.Fatalf("mute: expected 204, got %d", mw.Code)
	}

	// list and find our source
	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	w := httptest.NewRecorder()
	h.listSources(w, req)
	var sources []Source
	json.NewDecoder(w.Body).Decode(&sources)
	for _, src := range sources {
		if src.ID == s.ID {
			if src.MutedUntil == nil {
				t.Fatal("expected muted_until to be set after mute")
			}
			if src.MutedUntil.Before(time.Now()) {
				t.Fatal("muted_until should be in the future")
			}
			return
		}
	}
	t.Fatal("source not found in list")
}

func TestDeleteSource_OK(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "uptime", "ToDelete")

	req := httptest.NewRequest(http.MethodDelete, "/sources/"+itoa(s.ID), nil)
	req.URL.Path = "/sources/" + itoa(s.ID)
	w := httptest.NewRecorder()
	h.sourceByID(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestDeleteSource_NotFound(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/sources/999999", nil)
	w := httptest.NewRecorder()
	h.sourceByID(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteSource_InvalidID(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/sources/abc", nil)
	w := httptest.NewRecorder()
	h.sourceByID(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSources_MethodNotAllowed(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPut, "/sources", nil)
	w := httptest.NewRecorder()
	h.sources(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ── mute ─────────────────────────────────────────────────────────────────────

func TestMuteSource_1h(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "Mute1h")
	before := time.Now()

	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"1h"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, s.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	var mutedUntil *time.Time
	h.db.QueryRow(`SELECT muted_until FROM sources WHERE id=$1`, s.ID).Scan(&mutedUntil)
	if mutedUntil == nil {
		t.Fatal("muted_until should be set")
	}
	expected := before.Add(time.Hour)
	if mutedUntil.Before(expected.Add(-5*time.Second)) || mutedUntil.After(expected.Add(5*time.Second)) {
		t.Fatalf("muted_until %v not within 5s of expected %v", mutedUntil, expected)
	}
}

func TestMuteSource_Forever(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteForever")

	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"forever"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, s.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	var mutedUntil *time.Time
	h.db.QueryRow(`SELECT muted_until FROM sources WHERE id=$1`, s.ID).Scan(&mutedUntil)
	if mutedUntil == nil {
		t.Fatal("muted_until should be set")
	}
	if mutedUntil.Before(time.Now().Add(99 * 365 * 24 * time.Hour)) {
		t.Fatal("forever should be far in future")
	}
}

func TestMuteSource_UntilMorning(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteMorning")

	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"until_morning"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, s.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	var mutedUntil *time.Time
	h.db.QueryRow(`SELECT muted_until FROM sources WHERE id=$1`, s.ID).Scan(&mutedUntil)
	if mutedUntil == nil {
		t.Fatal("muted_until should be set")
	}
	if mutedUntil.Hour() != 9 || mutedUntil.Minute() != 0 {
		t.Fatalf("until_morning should be 09:00, got %02d:%02d", mutedUntil.Hour(), mutedUntil.Minute())
	}
	if !mutedUntil.After(time.Now()) {
		t.Fatal("until_morning should be in the future")
	}
}

func TestMuteSource_Unmute(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "Unmute")

	// first mute
	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"1h"}`))
	req.Header.Set("Content-Type", "application/json")
	h.muteSource(httptest.NewRecorder(), req, s.ID)

	// then unmute
	req2 := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"unmute"}`))
	req2.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req2, s.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	var mutedUntil *time.Time
	h.db.QueryRow(`SELECT muted_until FROM sources WHERE id=$1`, s.ID).Scan(&mutedUntil)
	if mutedUntil != nil {
		t.Fatal("muted_until should be NULL after unmute")
	}
}

func TestMuteSource_InvalidDuration(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteBad")

	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"notaduration"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, s.ID)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMuteSource_NegativeDuration(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteNeg")

	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{"duration":"-1h"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, s.ID)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMuteSource_NotFound(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPatch, "/sources/999999/mute",
		bytes.NewBufferString(`{"duration":"1h"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, 999999)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMuteSource_InvalidJSON(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteJSON")
	req := httptest.NewRequest(http.MethodPatch, "/sources/"+itoa(s.ID)+"/mute",
		bytes.NewBufferString(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.muteSource(w, req, s.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMuteEndpoint_MethodNotAllowed(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "rss", "MuteMethod")
	req := httptest.NewRequest(http.MethodGet, "/sources/"+itoa(s.ID)+"/mute", nil)
	req.URL.Path = "/sources/" + itoa(s.ID) + "/mute"
	w := httptest.NewRecorder()
	h.sourceByID(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ── events ────────────────────────────────────────────────────────────────────

func TestEvents_OK(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/events?limit=10", nil)
	w := httptest.NewRecorder()
	h.events(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var events []Event
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestEvents_LimitCapped(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/events?limit=9999", nil)
	w := httptest.NewRecorder()
	h.events(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestEvents_MethodNotAllowed(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	w := httptest.NewRecorder()
	h.events(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestMarkRead_OK(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "uptime", "ReadTest")

	// insert event directly
	var evID int64
	h.db.QueryRow(`INSERT INTO events (source_id, title, body, priority) VALUES ($1,'t','b','normal') RETURNING id`, s.ID).Scan(&evID)
	t.Cleanup(func() { h.db.Exec(`DELETE FROM events WHERE id=$1`, evID) })

	req := httptest.NewRequest(http.MethodPatch, "/events/"+itoa(evID)+"/read", nil)
	req.URL.Path = "/events/" + itoa(evID) + "/read"
	w := httptest.NewRecorder()
	h.eventByID(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var readAt *time.Time
	h.db.QueryRow(`SELECT read_at FROM events WHERE id=$1`, evID).Scan(&readAt)
	if readAt == nil {
		t.Fatal("read_at should be set after mark read")
	}
}

func TestMarkRead_AlreadyRead(t *testing.T) {
	h := setupHandler(t)
	s := createTestSource(t, h, "uptime", "ReadTest2")

	var evID int64
	h.db.QueryRow(`INSERT INTO events (source_id, title, body, priority) VALUES ($1,'t','b','normal') RETURNING id`, s.ID).Scan(&evID)
	t.Cleanup(func() { h.db.Exec(`DELETE FROM events WHERE id=$1`, evID) })

	// mark read twice
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPatch, "/events/"+itoa(evID)+"/read", nil)
		req.URL.Path = "/events/" + itoa(evID) + "/read"
		w := httptest.NewRecorder()
		h.eventByID(w, req)
		if i == 1 && w.Code != http.StatusNotFound {
			t.Fatalf("second mark-read: expected 404, got %d", w.Code)
		}
	}
}

func TestMarkRead_NotFound(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPatch, "/events/999999/read", nil)
	req.URL.Path = "/events/999999/read"
	w := httptest.NewRecorder()
	h.eventByID(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestMarkRead_InvalidID(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodPatch, "/events/abc/read", nil)
	req.URL.Path = "/events/abc/read"
	w := httptest.NewRecorder()
	h.eventByID(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMarkRead_MethodNotAllowed(t *testing.T) {
	h := setupHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/events/1/read", nil)
	req.URL.Path = "/events/1/read"
	w := httptest.NewRecorder()
	h.eventByID(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// ── queryInt ──────────────────────────────────────────────────────────────────

func TestQueryInt_Default(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	if got := queryInt(req, "limit", 50); got != 50 {
		t.Fatalf("expected 50, got %d", got)
	}
}

func TestQueryInt_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events?limit=25", nil)
	if got := queryInt(req, "limit", 50); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
}

func TestQueryInt_Invalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events?limit=abc", nil)
	if got := queryInt(req, "limit", 50); got != 50 {
		t.Fatalf("expected default 50, got %d", got)
	}
}

func TestQueryInt_Negative(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events?limit=-5", nil)
	if got := queryInt(req, "limit", 50); got != 50 {
		t.Fatalf("expected default 50, got %d", got)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
