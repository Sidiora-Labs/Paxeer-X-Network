package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// TypedHandler wraps a result-less typed handler into a TaskHandler. The
// map[string]any params are marshaled to JSON and unmarshaled into the typed
// struct T, giving handlers compile-time type safety without changing the
// engine's dispatch mechanism. Handlers that produce a structured result use
// TypedHandlerWithResult instead.
func TypedHandler[T any](fn func(ctx context.Context, params T) error) TaskHandler {
	return TypedHandlerWithResult(func(ctx context.Context, params T) (json.RawMessage, error) {
		return nil, fn(ctx, params)
	})
}

// TypedHandlerWithResult wraps a typed handler that returns a structured
// result into a TaskHandler. R is marshaled to json.RawMessage and returned
// alongside the error, so the engine persists it on both the success and
// error paths (an error return may still carry a meaningful R). A nil/zero R
// that marshals to "null" is treated as no result.
func TypedHandlerWithResult[T, R any](fn func(ctx context.Context, params T) (R, error)) TaskHandler {
	return func(ctx context.Context, params map[string]any) (json.RawMessage, error) {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshaling params: %w", err)
		}
		var typed T
		if err := json.Unmarshal(data, &typed); err != nil {
			return nil, fmt.Errorf("parsing params: %w", err)
		}
		result, ferr := fn(ctx, typed)
		raw, merr := json.Marshal(result)
		if merr != nil {
			// A result that won't marshal shouldn't mask the handler's own
			// outcome; surface it only when the handler otherwise succeeded.
			if ferr == nil {
				return nil, fmt.Errorf("marshaling result: %w", merr)
			}
			return nil, ferr
		}
		if string(raw) == "null" {
			raw = nil
		}
		return raw, ferr
	}
}
