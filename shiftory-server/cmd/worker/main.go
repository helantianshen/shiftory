package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"shiftory-server/internal/importer/imageai"
	"shiftory-server/internal/importjob"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
	"shiftory-server/internal/platform/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		log.Fatal(err)
	}
	store, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		log.Fatal(err)
	}
	client, err := imageai.NewOpenAICompatible(cfg.AIModel, cfg.AIAPIKey, cfg.AIBaseURL)
	if err != nil {
		log.Fatalf("configure image AI: %v", err)
	}
	processor := importjob.NewImageProcessor(db, store, client, cfg.AIModel)
	worker := importjob.NewWorker(importjob.NewMySQLRepository(db), processor, cfg.WorkerID, cfg.WorkerLease)
	log.Printf("Shiftory image worker %s started", cfg.WorkerID)
	if err := worker.Run(ctx, cfg.WorkerPollPeriod); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
