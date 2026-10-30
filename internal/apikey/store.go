package apikey

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Key struct {
	ID        int64
	Name      string
	Prefix    string
	CreatedAt time.Time
	RevokedAt *time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Create(ctx context.Context, name string) (string, Key, error) {
	secret := Generate()
	hash := Hash(secret)
	k := Key{Name: name, Prefix: DisplayPrefix(secret)}

	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (name, key_prefix, key_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		k.Name, k.Prefix, hash[:],
	).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return "", Key{}, fmt.Errorf("insert api key: %w", err)
	}
	return secret, k, nil
}

func (s *Store) List(ctx context.Context) ([]Key, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, key_prefix, created_at, revoked_at
		 FROM api_keys
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query api keys: %w", err)
	}

	keys, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Key, error) {
		var k Key
		err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.RevokedAt)
		return k, err
	})
	if err != nil {
		return nil, fmt.Errorf("read api keys: %w", err)
	}
	return keys, nil
}

func (s *Store) LoadActive(ctx context.Context) (map[[sha256.Size]byte]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, key_hash
		 FROM api_keys
		 WHERE revoked_at IS NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active api keys: %w", err)
	}

	active := make(map[[sha256.Size]byte]int64)
	var id int64
	var hash []byte
	_, err = pgx.ForEachRow(rows, []any{&id, &hash}, func() error {
		active[[sha256.Size]byte(hash)] = id
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read active api keys: %w", err)
	}
	return active, nil
}

var ErrNotActive = errors.New("no active api key with this id")

func (s *Store) Revoke(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys
		 SET revoked_at = now()
		 WHERE id = $1 AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotActive
	}
	return nil
}
