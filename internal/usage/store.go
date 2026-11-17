package usage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// usageColumns must be in the same order as the values in Insert. Postgres
// matches them by position, not by name.
var usageColumns = []string{
	"api_key_id", "started_at", "duration_ms", "model", "provider",
	"stream", "status_code", "outcome", "prompt_tokens", "completion_tokens",
}

// Insert writes all records in one COPY. COPY is all-or-nothing: if one row
// fails a constraint, no row is written.
func (s *Store) Insert(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"usage_logs"}, usageColumns,
		pgx.CopyFromSlice(len(records), func(i int) ([]any, error) {
			r := records[i]
			return []any{
				r.APIKeyID, r.StartedAt, r.Duration.Milliseconds(), r.Model, r.Provider,
				r.Stream, r.StatusCode, string(r.Outcome), r.PromptTokens, r.CompletionTokens,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("copy usage records: %w", err)
	}
	return nil
}
