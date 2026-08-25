package types

import "context"

// LLMCallObservation is the provider-independent telemetry emitted for one
// completed model call. Prompt content is intentionally excluded; only the
// stable prefix fingerprint is retained for cache analysis.
type LLMCallObservation struct {
	ModelID                 string
	ModelName               string
	Purpose                 string
	PromptPrefixFingerprint string
	Usage                   TokenUsage
	DurationMS              int64
	Success                 bool
	Error                   string
}

// LLMCallObserver receives completed model-call telemetry. Implementations
// must be safe for concurrent use because evaluation items run in parallel.
type LLMCallObserver interface {
	ObserveLLMCall(observation LLMCallObservation)
}

type llmCallObserverContextKey struct{}

// WithLLMCallObserver attaches a request-scoped model-call observer.
func WithLLMCallObserver(ctx context.Context, observer LLMCallObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, llmCallObserverContextKey{}, observer)
}

// LLMCallObserverFromContext returns the observer attached to ctx, if any.
func LLMCallObserverFromContext(ctx context.Context) (LLMCallObserver, bool) {
	if ctx == nil {
		return nil, false
	}
	observer, ok := ctx.Value(llmCallObserverContextKey{}).(LLMCallObserver)
	return observer, ok && observer != nil
}
