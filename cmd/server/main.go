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

	"nostr-app/internal/config"
	"nostr-app/internal/server"
)

func main() {
	cfg := config.FromEnv()
	apiHandlers := server.NewAPI(cfg.RelayURL)
	srv := server.NewWithAPI(cfg.Addr, cfg.StaticDir, apiHandlers)
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownDone := make(chan struct{})
	go func() {
		<-rootCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()

	log.Printf("starting server on %s serving %s", cfg.Addr, cfg.StaticDir)
	serverErr := srv.ListenAndServe()
	stop()
	<-shutdownDone

	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		log.Fatal(serverErr)
	}
}
