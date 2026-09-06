package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shiftory-server/internal/auth"
	"shiftory-server/internal/importer/imageai"
	"shiftory-server/internal/importjob"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
	"shiftory-server/internal/platform/httpserver"
	"shiftory-server/internal/platform/jwtkeys"
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
	store, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		log.Fatal(err)
	}
	privateKey, publicKey, err := jwtkeys.LoadOrCreate(cfg.JWTPrivateKey, cfg.JWTPublicKey)
	if err != nil {
		log.Fatal(err)
	}
	tokens := auth.NewTokenManager(privateKey, publicKey, "shiftory-ed25519-v1", cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	var runner *importjob.Runner
	if cfg.AIEnabled {
		client, err := imageai.NewOpenAICompatibleWithTimeout(cfg.AIModel, cfg.AIAPIKey, cfg.AIBaseURL, cfg.AIRequestTimeout)
		if err != nil {
			log.Fatalf("configure image AI: %v", err)
		}
		processor := importjob.NewImageProcessor(db, store, client, cfg.AIModel)
		worker := importjob.NewWorker(importjob.NewMySQLRepository(db), processor, cfg.WorkerID, cfg.WorkerLease)
		runner, err = importjob.NewRunner(worker, cfg.WorkerMaxConcurrency, cfg.WorkerPollPeriod)
		if err != nil {
			log.Fatalf("configure image worker: %v", err)
		}
	}
	var wakeup func()
	if runner != nil {
		wakeup = runner.Notify
	}
	handler, err := httpserver.New(httpserver.Dependencies{DB: db, Config: cfg, Tokens: tokens, Store: store, ImportWakeup: wakeup})
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	if runner != nil {
		go func() {
			if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("image worker stopped: %v", err)
			}
		}()
	}
	go func() {
		log.Printf("Shiftory API listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("API server stopped unexpectedly: %v", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	if runner != nil {
		runner.Close()
	}
}
