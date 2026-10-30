package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/db"
)

const usage = "usage: gatewayctl migrate"

func main() {
	flag.Parse()

	switch flag.Arg(0) {
	case "migrate":
		if err := migrate(); err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func migrate() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
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
