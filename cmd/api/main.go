package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
	"github.com/vladimir/notification-hub/internal/api"
	"github.com/vladimir/notification-hub/internal/db"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://hub:hub@localhost:5432/hub?sslmode=disable"
	}

	if err := db.RunMigrations(databaseURL, "migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Println("migrations applied")

	sqlDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	api.NewHandler(sqlDB).Register(mux)

	log.Println("api listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
