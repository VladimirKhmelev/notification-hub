package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/vladimir/notification-hub/internal/db"
	"github.com/vladimir/notification-hub/internal/natsutil"
	"github.com/vladimir/notification-hub/internal/uptime"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://hub:hub@localhost:5432/hub?sslmode=disable"
	}
	natsURL := os.Getenv("NATS_URL")

	if err := db.RunMigrations(databaseURL, "migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	sqlDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	nc, err := natsutil.Connect(natsURL)
	if err != nil {
		log.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	log.Println("uptime collector started, interval=30s")
	uptime.New(sqlDB, nc, 30*time.Second).Run()
}
