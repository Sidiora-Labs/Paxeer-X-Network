package livepeer

import (
	"context"
	"errors"
	"net/http"
	"regexp"
)

var correlationID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var ErrAmbiguousCorrelation = errors.New("livepeer: ambiguous stream correlation")
var ErrCorrelationMissing = errors.New("livepeer: stream correlation unresolved")

func CorrelationName(operation string) string {
	if !correlationID.MatchString(operation) {
		return ""
	}
	return "KindleLaunch:" + operation
}
func (c *Client) CreateCorrelated(ctx context.Context, operation string) (*CreateResponse, error) {
	name := CorrelationName(operation)
	if name == "" {
		return nil, ErrCorrelationMissing
	}
	created, err := c.CreateStream(ctx, name)
	if err != nil {
		return nil, err
	}
	if created.Name != name {
		return nil, ErrAmbiguousCorrelation
	}
	return created, nil
}
func (c *Client) FindCorrelated(ctx context.Context, operation string) (*Stream, error) {
	name := CorrelationName(operation)
	if name == "" {
		return nil, ErrCorrelationMissing
	}
	var streams []Stream
	if err := c.do(ctx, http.MethodGet, "/stream?streamsonly=true", nil, &streams); err != nil {
		return nil, err
	}
	return findCorrelation(streams, name)
}

func findCorrelation(streams []Stream, name string) (*Stream, error) {
	if streams == nil || len(streams) > 10000 {
		return nil, ErrAmbiguousCorrelation
	}
	var found *Stream
	for i := range streams {
		if streams[i].Name != name {
			continue
		}
		if found != nil {
			return nil, ErrAmbiguousCorrelation
		}
		if !safeID(streams[i].ID) || !safeID(streams[i].PlaybackID) || !safeID(streams[i].StreamKey) {
			return nil, ErrAmbiguousCorrelation
		}
		s := streams[i]
		found = &s
	}
	if found == nil {
		return nil, ErrCorrelationMissing
	}
	return found, nil
}
func (c *Client) Available(ctx context.Context) error {
	var streams []Stream
	if err := c.do(ctx, http.MethodGet, "/stream?streamsonly=true", nil, &streams); err != nil {
		return err
	}
	if streams == nil || len(streams) > 10000 {
		return ErrAmbiguousCorrelation
	}
	return nil
}
