package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestCreateSource_OK(t *testing.T) {
	h := setupHandler(t)

	body := `{"type":"uptime","name":"Test Site","config":{"url":"https://example.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/sources", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.createSource(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var s Source
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if s.ID == 0 {
		t.Fatal("expected non-zero ID in response")
	}
	if s.Name != "Test Site" {
		t.Fatalf("expected name %q, got %q", "Test Site", s.Name)
	}

	// cleanup
	h.db.Exec(`DELETE FROM sources WHERE id = $1`, s.ID)
}

func TestCreateSource_MissingFields(t *testing.T) {
	h := setupHandler(t)

	body := `{"type":"uptime"}`
	req := httptest.NewRequest(http.MethodPost, "/sources", bytes.NewBufferString(body))
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
		t.Fatalf("decode response: %v", err)
	}
}

func TestDeleteSource_NotFound(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/sources/999999", nil)
	w := httptest.NewRecorder()

	// manually set the path so sourceByID can parse it
	h.sourceByID(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

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
		t.Fatalf("decode response: %v", err)
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
