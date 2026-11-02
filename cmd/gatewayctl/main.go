package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dheerajsai15/LLM-Gateway/internal/apikey"
	"github.com/dheerajsai15/LLM-Gateway/internal/db"
)

const usage = `usage: gatewayctl migrate
       gatewayctl create-key <name>
       gatewayctl list-keys
       gatewayctl revoke-key <id>`

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
	case "list-keys":
		if err := listKeys(); err != nil {
			fmt.Fprintln(os.Stderr, "list-keys:", err)
			os.Exit(1)
		}
	case "revoke-key":
		keyID, err := strconv.ParseInt(flag.Arg(1), 10, 64)
		if err != nil || keyID <= 0 {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		if err := revokeKey(keyID); err != nil {
			fmt.Fprintln(os.Stderr, "revoke-key:", err)
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

func listKeys() error {
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

	keys, err := apikey.NewStore(pool).List(ctx)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tCREATED\tSTATUS")
	for _, k := range keys {
		status := "active"
		if k.RevokedAt != nil {
			status = "revoked " + k.RevokedAt.Local().Format(time.DateTime)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s...\t%s\t%s\n",
			k.ID, k.Name, k.Prefix, k.CreatedAt.Local().Format(time.DateTime), status)
	}
	return tw.Flush()
}

func revokeKey(keyID int64) error {
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

	err = apikey.NewStore(pool).Revoke(ctx, keyID)
	if errors.Is(err, apikey.ErrNotActive) {
		return fmt.Errorf("key %d: %w (unknown id or already revoked)", keyID, err)
	}
	if err != nil {
		return err
	}

	fmt.Printf("revoked key %d\n", keyID)
	return nil
}
