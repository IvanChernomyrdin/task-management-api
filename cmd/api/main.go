package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/auth"
	"github.com/IvanChernomyrdin/task-management-api/internal/config"
	"github.com/IvanChernomyrdin/task-management-api/internal/httpapi"
	"github.com/IvanChernomyrdin/task-management-api/internal/service"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		logger.Error("mysql init", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)

	if err = db.PingContext(ctx); err != nil {
		cancel()
		logger.Error("mysql connection", "error", err)
		os.Exit(1)
	}
	cancel()

	redisClient := redis.NewClient(
		&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		},
	)
	defer redisClient.Close()

	authManager := auth.Manager{
		Secret: []byte(cfg.JWTSecret),
		Issuer: cfg.JWTIssuer,
		TTL:    cfg.JWTDuration,
	}

	svc := service.New(db, redisClient, authManager, logger)

	api := httpapi.API{
		Service: svc,
		Logger:  logger,
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctxSignals, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	go func() {
		<-ctxSignals.Done()
		ctx, closeFn := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeFn()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("shutdown", "error", err)
		}
	}()

	logger.Info("listening", "address", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
