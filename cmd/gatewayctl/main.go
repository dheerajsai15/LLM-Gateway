package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/apikey"
	"github.com/dheerajsai15/LLM-Gateway/internal/db"
)

const usage = `usage: gatewayctl migrate
       gatewayctl create-key <name>`

func main() {
	flag.Parse()

	switch flag.Arg(0) {
	case "migrate":
		if err := migrate(); err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			os.Exit(1)
		}
	case "create-key":
		name := strings.TrimSpace(flag.Arg(1))
		if name == "" {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		if err := createKey(name); err != nil {
			fmt.Fprintln(os.Stderr, "create-key:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func databaseURL() (string, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return "", errors.New("DATABASE_URL is not set")
	}
	return dbURL, nil
}

func migrate() error {
	dbURL, err := databaseURL()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	applied, err := db.Migrate(ctx, dbURL)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		fmt.Println("database is up to date")
		return nil
	}
	fmt.Println("applied migrations:", applied)
	return nil
}

func createKey(name string) error {
	dbURL, err := databaseURL()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	secret, key, err := apikey.NewStore(pool).Create(ctx, name)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "created key %d (%s). Copy it now. It is not shown again.\n", key.ID, key.Name)
	fmt.Println(secret)
	return nil
}
