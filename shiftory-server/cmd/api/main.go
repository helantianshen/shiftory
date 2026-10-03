// Package main 启动长期运行的 HTTP API，并按配置承载 AI 导入任务
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"shiftory-server/internal/ai"
	"shiftory-server/internal/auth"
	"shiftory-server/internal/importjob"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
	"shiftory-server/internal/platform/httpserver"
	"shiftory-server/internal/platform/jwtkeys"
	"shiftory-server/internal/platform/logging"
	"shiftory-server/internal/platform/storage"
)

// main 加载配置并装配数据库、认证、存储和可选 AI 队列，处理服务启动与停机
func main() {
	// 先解析模式与配置再初始化日志，帮助请求直接结束而不连接外部资源
	cfg, err := config.Load(os.Args[1:]...)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if cfg.MigrateLegacyAI {
		log.Fatal("--migrate-ai-jobs is only valid for cmd/migrate")
	}
	logger, err := logging.New(cfg.LogLevel, cfg.LogFormat, nil)
	if err != nil {
		log.Fatal(err)
	}
	slog.SetDefault(logger)
	logger.Info("configuration loaded", "environment", cfg.Environment, "log_level", cfg.LogLevel, "log_format", cfg.LogFormat, "http_addr", cfg.Address(), "ai_enabled", cfg.AIEnabled)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 建立数据库、上传目录和持久化签名密钥，依赖失败时不开放 HTTP 服务
	db, err := database.Open(ctx, cfg.Postgres.DSN())
	if err != nil {
		logger.Error("database connection failed", "error", err)
		log.Fatal(err)
	}
	logger.Info("database connected")
	defer db.Close()
	// 表结构同步成功后才装配 Worker 并开放 HTTP 服务
	if err := database.Migrate(db); err != nil {
		log.Fatal(err)
	}
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
	redisClient := redis.NewClient(importjob.RedisOptions(cfg.Redis))
	defer redisClient.Close()
	var queue *importjob.Queue
	dispatcherCtx, stopDispatcher := context.WithCancel(context.Background())
	defer stopDispatcher()
	dispatcherDone := make(chan struct{})
	if cfg.AIEnabled {
		recognizer, e := ai.New(ctx, cfg.AI, redisClient)
		if e != nil {
			log.Fatal("AI workflow configuration failed")
		}
		processor := importjob.NewImageProcessorWithLogger(db, store, recognizer, "routed", logger)
		queue = importjob.NewQueue(db, cfg, processor, redisClient, logger)
		if e = queue.Start(); e != nil {
			log.Fatal("AI queue initialization failed")
		}
		go func() { defer close(dispatcherDone); queue.Dispatch(dispatcherCtx) }()
	} else {
		close(dispatcherDone)
	}

	handler, err := httpserver.New(httpserver.Dependencies{DB: db, Config: cfg, Tokens: tokens, Store: store, ImportCancel: func(c context.Context, j, g uint64) {
		if queue != nil {
			queue.Cancel(c, j, g)
		}
	}, QueueReady: func(c context.Context) bool { return queue != nil && queue.Ready(c) }, AIMetrics: func(c context.Context) (string, error) {
		if queue == nil {
			return "shiftory_ai_enabled 0\n", nil
		}
		return queue.Metrics(c)
	}, Logger: logger})
	if err != nil {
		logger.Error("HTTP handler initialization failed", "error", err)
		log.Fatal(err)
	}
	server := &http.Server{Addr: cfg.Address(), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
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
	stopDispatcher()
	if queue != nil {
		queue.Stop()
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	stopDispatcher()
	<-dispatcherDone
	if queue != nil {
		queue.Close()
	}
	logger.Info("Shiftory API stopped")
}
