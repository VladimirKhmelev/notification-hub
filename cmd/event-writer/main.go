package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"github.com/vladimir/notification-hub/internal/natsutil"
	"github.com/vladimir/notification-hub/internal/uptime"
)

// eventRow mirrors the events table row sent to WebSocket clients.
type eventRow struct {
	ID        int64     `json:"id"`
	SourceID  int64     `json:"source_id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Priority  string    `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://hub:hub@localhost:5432/hub?sslmode=disable"
	}
	natsURL := os.Getenv("NATS_URL")

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	nc, err := natsutil.Connect(natsURL)
	if err != nil {
		log.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	_, err = nc.Subscribe(natsutil.Subject, func(msg *nats.Msg) {
		var ev uptime.EventMsg
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			log.Printf("event-writer: bad message: %v", err)
			return
		}

		// skip if source is muted
		var mutedUntil *time.Time
		if err := db.QueryRow(
			`SELECT muted_until FROM sources WHERE id = $1`, ev.SourceID,
		).Scan(&mutedUntil); err != nil {
			log.Printf("event-writer: check mute for source %d: %v", ev.SourceID, err)
			return
		}
		if mutedUntil != nil && time.Now().Before(*mutedUntil) {
			log.Printf("event-writer: source %d is muted until %s, dropping event", ev.SourceID, mutedUntil.Format(time.RFC3339))
			return
		}

		var row eventRow
		err := db.QueryRow(
			`INSERT INTO events (source_id, title, body, priority)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, source_id, title, body, priority, created_at, read_at`,
			ev.SourceID, ev.Title, ev.Body, ev.Priority,
		).Scan(&row.ID, &row.SourceID, &row.Title, &row.Body, &row.Priority, &row.CreatedAt, &row.ReadAt)
		if err != nil {
			log.Printf("event-writer: insert event: %v", err)
			return
		}

		data, err := json.Marshal(row)
		if err != nil {
			log.Printf("event-writer: marshal row: %v", err)
			return
		}
		if err := nc.Publish(natsutil.InsertedSubject, data); err != nil {
			log.Printf("event-writer: publish inserted: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("nats subscribe: %v", err)
	}

	log.Printf("event-writer: subscribed to %s, publishing to %s", natsutil.Subject, natsutil.InsertedSubject)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("event-writer: shutting down")
}
