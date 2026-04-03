package llmclient

import (
	"context"
	"time"

	"github.com/bornholm/genai/llm"
	"github.com/bornholm/genai/llm/circuitbreaker"
	"github.com/bornholm/genai/llm/hook"
	"github.com/bornholm/genai/llm/provider"
	"github.com/bornholm/genai/llm/provider/env"
	"github.com/bornholm/genai/llm/provider/openrouter"
	"github.com/bornholm/genai/llm/ratelimit"
	"github.com/bornholm/genai/llm/retry"
	"github.com/pkg/errors"
)

// NewClient creates a resilient LLM client with retry, rate limiting and circuit breaker.
func NewClient(ctx context.Context) (llm.Client, error) {
	baseClient, err := provider.Create(ctx, env.With("GHOSTWRITER_", ".env"))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create llm client")
	}

	return Wrap(baseClient), nil
}

// Wrap adds retry, rate-limiting and circuit-breaker middleware to an existing client.
// Use this to apply the same resilience stack to secondary clients (e.g. the Corpus LLM client).
func Wrap(baseClient llm.Client) llm.Client {
	// Force middle-out transform for OpenRouter
	middleOutClient := hook.NewClient(baseClient, hook.WithBeforeChatCompletionFunc(func(ctx context.Context, funcs []llm.ChatCompletionOptionFunc) (context.Context, []llm.ChatCompletionOptionFunc, error) {
		ctx = openrouter.WithTransforms(ctx, []string{openrouter.TransformMiddleOut})
		return ctx, funcs, nil
	}))

	// 6 retries with 5s base delay (5s → 10s → 20s → 40s → 80s → 160s)
	retryClient := retry.NewClient(middleOutClient, 5*time.Second, 6)

	// Chat completion: 30 req/min — Embeddings: 60 req/min
	rateLimitedClient := ratelimit.NewClient(retryClient,
		ratelimit.WithChatLimit(time.Minute/30, 1),
		ratelimit.WithEmbeddingsLimit(time.Minute/60, 1),
	)

	// Circuit breaker: 5 failures max, 5s reset
	return circuitbreaker.NewClient(rateLimitedClient, 5, 5*time.Second)
}
