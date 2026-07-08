package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/vladimir/notification-hub/internal/db"
	"github.com/vladimir/notification-hub/internal/uptime"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://hub:hub@localhost:5432/hub?sslmode=disable"
	}

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

	log.Println("uptime collector started, interval=30s")
	uptime.New(sqlDB, 30*time.Second).Run()
}
