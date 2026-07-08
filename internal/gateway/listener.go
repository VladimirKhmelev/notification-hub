package gateway

import (
	"context"
	"log"
	"time"

	"github.com/lib/pq"
)

// listenNotify subscribes to Postgres LISTEN/NOTIFY on channel "events"
// and broadcasts each payload to the hub.
func listenNotify(ctx context.Context, databaseURL string, h *hub) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := listen(ctx, databaseURL, h); err != nil {
			log.Printf("ws-gateway: listener error: %v — retrying in 5s", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func listen(ctx context.Context, databaseURL string, h *hub) error {
	l := pq.NewListener(databaseURL, 5*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("ws-gateway: pq listener event: %v", err)
		}
	})
	if err := l.Listen("events"); err != nil {
		return err
	}
	defer l.Close()

	log.Println("ws-gateway: listening on postgres channel 'events'")
	for {
		select {
		case <-ctx.Done():
			return nil
		case n, ok := <-l.Notify:
			if !ok {
				return nil
			}
			if n == nil {
				continue
			}
			h.broadcast(ctx, []byte(n.Extra))
		case <-time.After(90 * time.Second):
			if err := l.Ping(); err != nil {
				return err
			}
		}
	}
}
