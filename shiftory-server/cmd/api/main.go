// Package main 启动长期运行的 HTTP API，并按配置承载图片导入任务
package main

import (
	"context"
	"errors"
	"flag"
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
	"shiftory-server/internal/platform/logging"
	"shiftory-server/internal/platform/storage"
)

// main 加载配置并装配数据库、认证、存储和可选图片 Runner，处理服务启动与停机
func main() {
	// 先解析模式与配置再初始化日志，帮助请求直接结束而不连接外部资源
	cfg, err := config.Load(os.Args[1:]...)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	logger, err := logging.New(cfg.LogLevel, cfg.LogFormat, nil)
	if err != nil {
		log.Fatal(err)
	}
	logger.Info("configuration loaded", "environment", cfg.Environment, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat, "http_addr", cfg.Address(), "ai_enabled", cfg.AIEnabled)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 建立数据库、上传目录和持久化签名密钥，依赖失败时不开放 HTTP 服务
	db, err := database.Open(ctx, cfg.MySQL.DSN())
	if err != nil {
		logger.Error("database connection failed", "error", err)
		log.Fatal(err)
	}
	logger.Info("database connected")
	defer db.Close()
	store, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		logger.Error("file storage initialization failed", "error", err)
		log.Fatal(err)
	}
	privateKey, publicKey, err := jwtkeys.LoadOrCreate(cfg.JWTPrivateKey, cfg.JWTPublicKey)
	if err != nil {
		logger.Error("JWT key initialization failed", "error", err)
		log.Fatal(err)
	}
	tokens := auth.NewTokenManager(privateKey, publicKey, "shiftory-ed25519-v1", cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	// AI 开关决定是否装配任务处理依赖，普通接口不要求识别器存在
	var runner *importjob.Runner
	if cfg.AIEnabled {
		client, err := imageai.NewOpenAICompatibleWithTimeoutAndLogger(cfg.AIModel, cfg.AIAPIKey, cfg.AIBaseURL, cfg.AIRequestTimeout, logger)
		if err != nil {
			log.Fatalf("configure image AI: %v", err)
		}
		processor := importjob.NewImageProcessorWithLogger(db, store, client, cfg.AIModel, logger)
		worker := importjob.NewWorker(importjob.NewMySQLRepository(db), processor, cfg.WorkerID, cfg.WorkerLease)
		runner, err = importjob.NewRunnerWithLogger(worker, cfg.WorkerMaxConcurrency, cfg.WorkerPollPeriod, logger)
		if err != nil {
			logger.Error("image worker configuration failed", "error", err)
			log.Fatalf("configure image worker: %v", err)
		}
		logger.Info("image worker enabled", "worker_id", cfg.WorkerID, "max_concurrency", cfg.WorkerMaxConcurrency, "poll_interval", cfg.WorkerPollPeriod.String(), "lease", cfg.WorkerLease.String())
	} else {
		logger.Info("image worker disabled")
	}
	// 把任务入库后的唤醒能力交给 HTTP 层，实际领取仍由 Runner 查询数据库
	var wakeup func()
	if runner != nil {
		wakeup = runner.Notify
	}
	handler, err := httpserver.New(httpserver.Dependencies{DB: db, Config: cfg, Tokens: tokens, Store: store, ImportWakeup: wakeup, Logger: logger})
	if err != nil {
		logger.Error("HTTP handler initialization failed", "error", err)
		log.Fatal(err)
	}
	server := &http.Server{Addr: cfg.Address(), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	if runner != nil {
		go func() {
			if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("image worker stopped", "error", err)
			}
		}()
	}
	// HTTP 监听在后台运行，异常退出会取消共享上下文以触发停机
	go func() {
		logger.Info("Shiftory API listening", "http_addr", cfg.Address())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("API server stopped unexpectedly", "error", err)
			stop()
		}
	}()
	// 收到退出信号后限时等待 HTTP 请求结束，再释放任务池和数据库资源
	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	if runner != nil {
		runner.Close()
	}
	logger.Info("Shiftory API stopped")
}
