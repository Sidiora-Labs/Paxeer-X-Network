package layerx

import (
	"context"
	"encoding/json"
	"sync"
)

type StreamCursor struct{ value string }

func NewStreamCursor(value string) (StreamCursor, error) {
	if value == "" || len(value) > 512 {
		return StreamCursor{}, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	for index := range value {
		if value[index] == 0 {
			return StreamCursor{}, newSDKError(ErrorInvalidArgument, RetryNever)
		}
	}
	return StreamCursor{value: value}, nil
}

func (cursor StreamCursor) String() string { return cursor.value }

type StreamEvent struct {
	Cursor       string               `json:"cursor"`
	Kind         HumanStreamEventKind `json:"kind"`
	ObservedAt   string               `json:"observed_at"`
	Journey      json.RawMessage      `json:"journey,omitempty"`
	Approval     json.RawMessage      `json:"approval,omitempty"`
	Notification json.RawMessage      `json:"notification,omitempty"`
}

type StreamPage struct {
	Events     []StreamEvent `json:"events"`
	NextCursor string        `json:"next_cursor"`
}

type StreamSource interface {
	Next(context.Context, StreamCursor) (StreamPage, error)
}

type HumanStreamSource struct{ client *Client }

func NewHumanStreamSource(client *Client) (*HumanStreamSource, error) {
	if client == nil {
		return nil, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	return &HumanStreamSource{client: client}, nil
}

func (source *HumanStreamSource) Open(ctx context.Context) (StreamCursor, error) {
	var position struct {
		Cursor string `json:"cursor"`
	}
	if err := source.client.Human(ctx, HumanOperationStreamOpen, nil, &position, CallOptions{}); err != nil {
		return StreamCursor{}, err
	}
	return NewStreamCursor(position.Cursor)
}

func (source *HumanStreamSource) Next(ctx context.Context, cursor StreamCursor) (StreamPage, error) {
	if cursor.value == "" {
		return StreamPage{}, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	var page StreamPage
	err := source.client.Human(ctx, HumanOperationStreamNext, nil, &page, CallOptions{
		PathParameters: map[string]string{"cursor": cursor.String()},
	})
	return page, err
}

type ResumableStream struct {
	mu     sync.Mutex
	cursor StreamCursor
	seen   map[string]struct{}
	source StreamSource
}

func NewResumableStream(cursor StreamCursor, source StreamSource) (*ResumableStream, error) {
	if cursor.value == "" || source == nil {
		return nil, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	return &ResumableStream{cursor: cursor, source: source, seen: make(map[string]struct{})}, nil
}

func (stream *ResumableStream) Cursor() StreamCursor {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.cursor
}

func (stream *ResumableStream) Next(ctx context.Context) ([]StreamEvent, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	page, err := stream.source.Next(ctx, stream.cursor)
	if err != nil {
		return nil, err
	}
	accepted := make([]StreamEvent, 0, len(page.Events))
	pageSeen := make(map[string]struct{}, len(page.Events))
	for _, event := range page.Events {
		if event.Cursor == "" || event.Cursor == stream.cursor.value {
			return nil, newSDKError(ErrorDecodeFailure, RetryNever)
		}
		if _, duplicate := stream.seen[event.Cursor]; duplicate {
			return nil, newSDKError(ErrorDecodeFailure, RetryNever)
		}
		if _, duplicate := pageSeen[event.Cursor]; duplicate {
			return nil, newSDKError(ErrorDecodeFailure, RetryNever)
		}
		pageSeen[event.Cursor] = struct{}{}
		accepted = append(accepted, event)
	}
	expected := stream.cursor.value
	if len(accepted) != 0 {
		expected = accepted[len(accepted)-1].Cursor
	}
	if page.NextCursor != expected {
		return nil, newSDKError(ErrorDecodeFailure, RetryNever)
	}
	next, err := NewStreamCursor(page.NextCursor)
	if err != nil {
		return nil, err
	}
	for cursor := range pageSeen {
		stream.seen[cursor] = struct{}{}
	}
	stream.cursor = next
	return accepted, nil
}

func (stream *ResumableStream) Run(ctx context.Context, consume func(StreamEvent) error) error {
	if consume == nil {
		return newSDKError(ErrorInvalidArgument, RetryNever)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := stream.Next(ctx)
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := consume(event); err != nil {
				return err
			}
		}
	}
}
