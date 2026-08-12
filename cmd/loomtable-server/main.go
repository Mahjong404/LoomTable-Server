package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/catalog"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/httpapi"
	loomrecord "github.com/Mahjong404/LoomTable-Server/internal/record"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func main() {
	cfg := config.Load()

	var ready httpapi.ReadyChecker
	var dependencies httpapi.Dependencies
	if cfg.DatabaseURL == "" {
		ready = postgres.ReadyChecker(nil)
	} else {
		db, err := postgres.Open(cfg.DatabaseURL)
		if err != nil {
			log.Printf("database unavailable: %v", err)
			ready = postgres.ReadyChecker(nil)
		} else {
			defer db.Close()
			ready = postgres.ReadyChecker(db)
			repository := postgres.NewRepository(db)
			dependencies = httpapi.Dependencies{
				Authenticator: repository,
				Bootstrap:     repository,
				Catalog:       catalog.New(repository),
				Records:       loomrecord.New(repository),
			}
		}
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(cfg, ready, dependencies).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("LoomTable Server listening on %s", cfg.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
	}
}
