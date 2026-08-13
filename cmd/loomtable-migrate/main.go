package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func main() {
	cfg := config.Load()
	directory := flag.String("dir", "migrations", "directory containing SQL migrations")
	flag.Parse()

	db, err := postgres.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := postgres.ApplyMigrations(ctx, db, *directory); err != nil {
		log.Fatal(err)
	}
	log.Printf("migrations applied")
}
