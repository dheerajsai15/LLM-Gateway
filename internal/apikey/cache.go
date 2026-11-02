package apikey

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"sync/atomic"
	"time"
)

type Cache struct {
	keys  atomic.Pointer[map[[sha256.Size]byte]int64]
	store *Store
}

const refreshTimeout = 10 * time.Second

func NewCache(store *Store) *Cache {
	return &Cache{store: store}
}

func (c *Cache) Lookup(secret string) (int64, bool) {
	m := c.keys.Load()
	if m == nil {
		return 0, false
	}

	id, ok := (*m)[Hash(secret)]
	return id, ok
}

func (c *Cache) Refresh(ctx context.Context) error {
	active, err := c.store.LoadActive(ctx)
	if err != nil {
		return err
	}

	c.keys.Store(&active)
	return nil
}

func (c *Cache) RefreshEvery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, refreshTimeout)
			err := c.Refresh(refreshCtx)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("api key refresh failed, keeping previous keys", "err", err)
			}
		}
	}
}
