package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/vladimir/notification-hub/internal/gateway"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://hub:hub@localhost:5432/hub?sslmode=disable"
	}

	ctx := context.Background()
	srv := gateway.NewServer(ctx, databaseURL)

	mux := http.NewServeMux()
	mux.Handle("/ws", srv)

	log.Println("ws-gateway listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}
