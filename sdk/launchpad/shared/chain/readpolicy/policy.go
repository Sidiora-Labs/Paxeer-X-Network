// Package readpolicy contains bounded, cancellation-aware read primitives.
package readpolicy

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/ethereum/go-ethereum/rpc"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const DefaultDeadline = 1500 * time.Millisecond
const MaxBatch = 100
const MaxRange uint64 = 1000
const MaxResponseBytes int64 = 8 << 20

type ReadError struct {
	Code           string `json:"code"`
	Retryable      bool   `json:"retryable"`
	EndpointID     string `json:"endpointId"`
	Method         string `json:"method"`
	RevertSelector string `json:"revertSelector,omitempty"`
	cause          error
}

func (e *ReadError) Error() string { return e.Code + ": " + e.Method + " at " + e.EndpointID }
func (e *ReadError) Unwrap() error { return e.cause }
func Classify(err error, endpoint, method string) *ReadError {
	e := &ReadError{Code: "RPC_REJECTED", EndpointID: endpoint, Method: method, cause: err}
	if errors.Is(err, context.Canceled) {
		e.Code = "CANCELED"
		return e
	}
	var re rpc.Error
	if errors.As(err, &re) {
		if re.ErrorCode() == 3 {
			e.Code = "SEMANTIC_REVERT"
			var de rpc.DataError
			if errors.As(err, &de) {
				if data, ok := de.ErrorData().(string); ok && len(data) >= 10 && strings.HasPrefix(data, "0x") {
					if _, err := hex.DecodeString(data[2:10]); err == nil {
						e.RevertSelector = strings.ToLower(data[:10])
					}
				}
			}
		}
		return e
	}
	var ne net.Error
	var he rpc.HTTPError
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &ne) {
		e.Code = "RPC_UNAVAILABLE"
		e.Retryable = true
	}
	if errors.As(err, &he) && (he.StatusCode == 429 || he.StatusCode >= 500) {
		e.Code = "RPC_UNAVAILABLE"
		e.Retryable = true
	}
	return e
}

type Range struct{ From, To uint64 }

// WalkRanges covers each height once on successful completion. A failed one-block
// range is returned to the caller, never skipped. Limits prevent unbounded work.
func WalkRanges(ctx context.Context, from, to, max uint64, read func(context.Context, Range) error, isLimit func(error) bool) error {
	if from > to || max == 0 || max > MaxRange || to-from >= 1000000 {
		return errors.New("invalid range budget")
	}
	var walk func(Range) error
	walk = func(r Range) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		e := read(ctx, r)
		if e == nil {
			return nil
		}
		if !isLimit(e) || r.From == r.To {
			return e
		}
		mid := r.From + (r.To-r.From)/2
		if e = walk(Range{r.From, mid}); e != nil {
			return e
		}
		return walk(Range{mid + 1, r.To})
	}
	for from <= to {
		end := from + max - 1
		if end < from || end > to {
			end = to
		}
		if e := walk(Range{from, end}); e != nil {
			return e
		}
		if end == to {
			break
		}
		from = end + 1
	}
	return nil
}

type BatchResponse struct {
	ID    uint64
	Value []byte
}

func MatchBatch(ids []uint64, responses []BatchResponse) ([][]byte, error) {
	if len(ids) == 0 || len(ids) > MaxBatch || len(ids) != len(responses) {
		return nil, errors.New("invalid batch size")
	}
	index := map[uint64]int{}
	for i, id := range ids {
		if _, ok := index[id]; ok {
			return nil, errors.New("duplicate request id")
		}
		index[id] = i
	}
	out := make([][]byte, len(ids))
	seen := map[uint64]bool{}
	var size int64
	for _, r := range responses {
		i, ok := index[r.ID]
		size += int64(len(r.Value))
		if !ok || seen[r.ID] || size > MaxResponseBytes {
			return nil, errors.New("invalid batch response")
		}
		seen[r.ID] = true
		out[i] = append([]byte(nil), r.Value...)
	}
	return out, nil
}

// ResponseTransport bounds bytes before JSON decoding, including error bodies.
type ResponseTransport struct{ Base http.RoundTripper }

func (t ResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = &limitedBody{body: resp.Body, remaining: MaxResponseBytes}
	return resp, nil
}

type limitedBody struct {
	body      io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var one [1]byte
		n, err := b.body.Read(one[:])
		if n > 0 {
			return 0, errors.New("RPC response exceeds byte budget")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	return n, err
}
func (b *limitedBody) Close() error { return b.body.Close() }
func IsRangeLimit(err error) bool {
	var re rpc.Error
	return errors.As(err, &re) && re.ErrorCode() == -32005
}
