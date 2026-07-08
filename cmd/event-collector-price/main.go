package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/vladimir/notification-hub/internal/natsutil"
	"github.com/vladimir/notification-hub/internal/price"
)

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

	log.Println("price collector started, interval=5m")
	price.New(db, nc, 5*time.Minute).Run()
}
