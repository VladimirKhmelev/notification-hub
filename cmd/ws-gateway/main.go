package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/vladimir/notification-hub/internal/gateway"
)

func main() {
	natsURL := os.Getenv("NATS_URL")

	ctx := context.Background()
	srv := gateway.NewServer(ctx, natsURL)

	mux := http.NewServeMux()
	mux.Handle("/ws", srv)

	log.Println("ws-gateway listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}
