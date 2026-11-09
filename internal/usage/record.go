package usage

import "time"

// Outcome tells how a request ended. The values must match the CHECK
// constraint on usage_logs.outcome.
type Outcome string

const (
	OutcomeOK             Outcome = "ok"
	OutcomeClientCanceled Outcome = "client_canceled"
	OutcomeIdleTimeout    Outcome = "idle_timeout"
	OutcomeUpstreamError  Outcome = "upstream_error"
	OutcomeBadRequest     Outcome = "bad_request"
)

// Record is one row of usage_logs. A nil token count is stored as NULL,
// which means that the count is not known.
type Record struct {
	APIKeyID         int64
	StartedAt        time.Time
	Duration         time.Duration
	Model            string
	Provider         string
	Stream           bool
	StatusCode       int
	Outcome          Outcome
	PromptTokens     *int
	CompletionTokens *int
}
