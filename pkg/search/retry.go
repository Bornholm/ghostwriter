package search

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/pkg/errors"
)

type Retry struct {
	client     Client
	baseDelay  time.Duration
	maxRetries int
}

// Search implements Client.
func (r *Retry) Search(ctx context.Context, search string) ([]Result, error) {
	backoff := r.baseDelay
	retries := 0
	for {
		results, err := r.client.Search(ctx, search)
		if err != nil {
			if retries < r.maxRetries {
				slog.WarnContext(ctx, "search failed, will retry", slog.Duration("backoff", backoff), slog.Int("retries", retries), slog.Any("error", errors.WithStack(err)))
				time.Sleep(backoff + time.Duration(rand.Float64()*float64(r.baseDelay)))
				backoff *= 2
				retries++
				continue
			}

			return nil, errors.WithStack(err)
		}

		return results, nil
	}
}

var _ Client = &Retry{}

func WithRetry(client Client, maxRetries int, baseDelay time.Duration) *Retry {
	return &Retry{client: client, maxRetries: maxRetries, baseDelay: baseDelay}
}
