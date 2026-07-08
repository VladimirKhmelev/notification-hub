package gateway

import (
	"context"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vladimir/notification-hub/internal/natsutil"
)

// listenNATS subscribes to events.inserted and broadcasts each payload to the hub.
func listenNATS(ctx context.Context, natsURL string, h *hub) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := subscribe(ctx, natsURL, h); err != nil {
			log.Printf("ws-gateway: nats listener error: %v — retrying in 5s", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func subscribe(ctx context.Context, natsURL string, h *hub) error {
	nc, err := natsutil.Connect(natsURL)
	if err != nil {
		return err
	}
	defer nc.Close()

	sub, err := nc.Subscribe(natsutil.InsertedSubject, func(msg *nats.Msg) {
		h.broadcast(ctx, msg.Data)
	})
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	log.Printf("ws-gateway: subscribed to NATS subject %s", natsutil.InsertedSubject)

	<-ctx.Done()
	return nil
}
