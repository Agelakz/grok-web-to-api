package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grok-web-to-api/internal/config"
	"grok-web-to-api/internal/grok"
	"grok-web-to-api/internal/server"
	"grok-web-to-api/pkg/logger"

	"go.uber.org/zap"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		panic(err)
	}

	log, err := logger.New(cfg.LogLevel)
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	client := grok.NewClient(cfg, log)
	if err := client.Init(context.Background()); err != nil {
		log.Fatal("grok init failed", zap.Error(err))
	}
	defer client.Close() //nolint:errcheck

	srv := server.New(cfg, log, client)

	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
