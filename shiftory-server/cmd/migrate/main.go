package main

import (
	"context"
	"log"

	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(context.Background(), cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		log.Fatal(err)
	}
	log.Println("Shiftory migrations are up to date")
}
