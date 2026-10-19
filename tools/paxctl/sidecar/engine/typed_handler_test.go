package engine

import (
	"context"
	"testing"
)

func TestTypedHandler_HappyPath(t *testing.T) {
	type req struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	var captured req
	handler := TypedHandler(func(_ context.Context, r req) error {
		captured = r
		return nil
	})

	_, err := handler(context.Background(), map[string]any{
		"name": "alice",
		"age":  float64(30),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Name != "alice" {
		t.Errorf("Name = %q, want %q", captured.Name, "alice")
	}
	if captured.Age != 30 {
		t.Errorf("Age = %d, want 30", captured.Age)
	}
}

func TestTypedHandler_MalformedJSON(t *testing.T) {
	type req struct {
		Value int `json:"value"`
	}

	handler := TypedHandler(func(_ context.Context, r req) error {
		return nil
	})

	// A channel cannot be marshaled to JSON, causing a marshal error.
	_, err := handler(context.Background(), map[string]any{
		"value": make(chan int),
	})
	if err == nil {
		t.Fatal("expected error for un-marshalable params")
	}
}

func TestTypedHandler_NilParams(t *testing.T) {
	type req struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	var captured req
	handler := TypedHandler(func(_ context.Context, r req) error {
		captured = r
		return nil
	})

	_, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error for nil params: %v", err)
	}
	if captured.Name != "" {
		t.Errorf("Name = %q, want empty string", captured.Name)
	}
	if captured.Age != 0 {
		t.Errorf("Age = %d, want 0", captured.Age)
	}
}

func TestTypedHandler_EmptyParams(t *testing.T) {
	type req struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	var captured req
	handler := TypedHandler(func(_ context.Context, r req) error {
		captured = r
		return nil
	})

	_, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error for empty params: %v", err)
	}
	if captured.Name != "" {
		t.Errorf("Name = %q, want empty string", captured.Name)
	}
	if captured.Age != 0 {
		t.Errorf("Age = %d, want 0", captured.Age)
	}
}

func TestTypedHandler_NestedStruct(t *testing.T) {
	type inner struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	type req struct {
		Nested inner `json:"nested"`
	}

	var captured req
	handler := TypedHandler(func(_ context.Context, r req) error {
		captured = r
		return nil
	})

	_, err := handler(context.Background(), map[string]any{
		"nested": map[string]any{
			"key":   "foo",
			"value": "bar",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Nested.Key != "foo" {
		t.Errorf("Nested.Key = %q, want %q", captured.Nested.Key, "foo")
	}
	if captured.Nested.Value != "bar" {
		t.Errorf("Nested.Value = %q, want %q", captured.Nested.Value, "bar")
	}
}

func TestTypedHandler_Float64ToInt64Coercion(t *testing.T) {
	type req struct {
		Height int64 `json:"height"`
	}

	var captured req
	handler := TypedHandler(func(_ context.Context, r req) error {
		captured = r
		return nil
	})

	// JSON numbers arrive as float64 when unmarshaled into map[string]any.
	_, err := handler(context.Background(), map[string]any{
		"height": float64(198030000),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Height != 198030000 {
		t.Errorf("Height = %d, want 198030000", captured.Height)
	}
}
