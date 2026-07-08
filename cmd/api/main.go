package main

import (
	"log"
	"net/http"
	"os"

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

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Println("api listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
