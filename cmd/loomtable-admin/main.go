package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	loomauth "github.com/Mahjong404/LoomTable-Server/internal/auth"
	"github.com/Mahjong404/LoomTable-Server/internal/config"
	"github.com/Mahjong404/LoomTable-Server/internal/storage/postgres"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 || args[0] != "auth" {
		printUsage(stderr)
		return errors.New("expected an auth subcommand")
	}
	cfg := config.Load()
	db, err := postgres.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := loomauth.NewAdmin(postgres.NewRepository(db))

	switch args[1] {
	case "bootstrap":
		flags := flag.NewFlagSet("auth bootstrap", flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := flags.String("name", "", "name for the initial Token")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("auth bootstrap does not accept positional arguments")
		}
		result, err := admin.Bootstrap(ctx, *name)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "create":
		flags := flag.NewFlagSet("auth create", flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := flags.String("name", "", "name for the new Token")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("auth create does not accept positional arguments")
		}
		issued, err := admin.Create(ctx, *name)
		if err != nil {
			return err
		}
		return writeJSON(stdout, issued)
	case "list":
		if len(args) != 2 {
			return errors.New("auth list does not accept arguments")
		}
		items, err := admin.List(ctx)
		if err != nil {
			return err
		}
		return writeJSON(stdout, map[string]any{"items": items})
	case "revoke":
		flags := flag.NewFlagSet("auth revoke", flag.ContinueOnError)
		flags.SetOutput(stderr)
		tokenID := flags.String("token-id", "", "typed tok_ ID to revoke")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("auth revoke does not accept positional arguments")
		}
		revoked, err := admin.Revoke(ctx, *tokenID)
		if err != nil {
			return err
		}
		return writeJSON(stdout, revoked)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown auth subcommand %q", args[1])
	}
}

func writeJSON(destination io.Writer, value any) error {
	encoder := json.NewEncoder(destination)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printUsage(destination io.Writer) {
	fmt.Fprintln(destination, "usage: loomtable-admin auth bootstrap --name NAME")
	fmt.Fprintln(destination, "       loomtable-admin auth create --name NAME")
	fmt.Fprintln(destination, "       loomtable-admin auth list")
	fmt.Fprintln(destination, "       loomtable-admin auth revoke --token-id tok_...")
}
