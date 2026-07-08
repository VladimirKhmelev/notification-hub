package api

import (
	"encoding/json"
	"time"
)

type Source struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
}

type Event struct {
	ID        int64      `json:"id"`
	SourceID  int64      `json:"source_id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Priority  string     `json:"priority"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}
