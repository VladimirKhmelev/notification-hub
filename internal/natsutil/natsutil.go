package natsutil

import (
	"time"

	"github.com/nats-io/nats.go"
)

// Connect connects to NATS with retries. url defaults to nats.DefaultURL if empty.
func Connect(url string) (*nats.Conn, error) {
	if url == "" {
		url = nats.DefaultURL
	}
	return nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
}

// Subject is the NATS subject all event collectors publish to.
const Subject = "events.raw"

// InsertedSubject is published by event-writer after a successful DB insert.
// ws-gateway subscribes to this to broadcast to WebSocket clients.
const InsertedSubject = "events.inserted"
