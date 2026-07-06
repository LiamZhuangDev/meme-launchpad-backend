package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/meme-launchpad/app-rebuild/internal/app"
	"github.com/meme-launchpad/app-rebuild/internal/config"
)

func main() {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startupCancel()

	application, err := app.New(startupCtx, cfg)
	if err != nil {
		log.Fatalf("start application: %v", err)
	}
	defer application.Close()
	server := application.HTTPServer()
	serverErrors := make(chan error, 1)

	go func() {
		log.Printf("%s REST listening on http://%s", cfg.ServiceName, cfg.HTTP.Address())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- fmt.Errorf("HTTP server: %w", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case <-signals:
	case err := <-serverErrors:
		log.Printf("server failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
