package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	db *sql.DB
}

func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/sources", h.sources)
	mux.HandleFunc("/sources/", h.sourceByID)
	mux.HandleFunc("/events", h.events)
	mux.HandleFunc("/events/", h.eventByID)
}

// POST /sources        — create
// GET  /sources        — list
func (h *Handler) sources(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listSources(w, r)
	case http.MethodPost:
		h.createSource(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// DELETE /sources/{id}       — delete
// PATCH  /sources/{id}/mute  — mute source
func (h *Handler) sourceByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/sources/")
	parts := strings.SplitN(path, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if len(parts) == 2 {
		if parts[1] != "mute" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.muteSource(w, r, id)
		return
	}

	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	res, err := h.db.Exec(`DELETE FROM sources WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) muteSource(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		Duration string `json:"duration"` // "1h", "8h", "forever", or "unmute"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	var mutedUntil *time.Time
	switch req.Duration {
	case "unmute":
		// mutedUntil stays nil — clears the mute
	case "forever":
		t := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
		mutedUntil = &t
	case "until_morning":
		now := time.Now()
		morning := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
		mutedUntil = &morning
	default:
		d, err := time.ParseDuration(req.Duration)
		if err != nil || d <= 0 {
			http.Error(w, "invalid duration", http.StatusBadRequest)
			return
		}
		t := time.Now().Add(d)
		mutedUntil = &t
	}

	res, err := h.db.Exec(`UPDATE sources SET muted_until = $1 WHERE id = $2`, mutedUntil, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /events?limit=50&offset=0
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	if limit > 200 {
		limit = 200
	}

	rows, err := h.db.Query(`
		SELECT id, source_id, title, body, priority, created_at, read_at
		FROM events
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.SourceID, &e.Title, &e.Body, &e.Priority, &e.CreatedAt, &e.ReadAt); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, events)
}

func (h *Handler) listSources(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`SELECT id, type, name, config, created_at, muted_until FROM sources ORDER BY created_at DESC`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sources := make([]Source, 0)
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.Type, &s.Name, &s.Config, &s.CreatedAt, &s.MutedUntil); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, sources)
}

func (h *Handler) createSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type   string          `json:"type"`
		Name   string          `json:"name"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Type == "" || req.Name == "" {
		http.Error(w, "type and name are required", http.StatusBadRequest)
		return
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage("{}")
	}

	var s Source
	err := h.db.QueryRow(`
		INSERT INTO sources (type, name, config)
		VALUES ($1, $2, $3)
		RETURNING id, type, name, config, created_at, muted_until`,
		req.Type, req.Name, req.Config,
	).Scan(&s.ID, &s.Type, &s.Name, &s.Config, &s.CreatedAt, &s.MutedUntil)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, s)
}

// PATCH /events/{id}/read — mark event as read
func (h *Handler) eventByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/events/"), "/", 3)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if len(parts) < 2 || parts[1] != "read" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	res, err := h.db.Exec(
		`UPDATE events SET read_at = NOW() WHERE id = $1 AND read_at IS NULL`, id,
	)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found or already read", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}
