package readpolicy

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type revert struct{}

func (revert) Error() string  { return "OpticalQuoteUnsupported" }
func (revert) ErrorCode() int { return 3 }
func TestClassification(t *testing.T) {
	if Classify(revert{}, "p", "call").Retryable {
		t.Fatal("revert retried")
	}
	if !Classify(io.EOF, "p", "call").Retryable {
		t.Fatal("transport not retried")
	}
	if Classify(context.Canceled, "p", "call").Retryable {
		t.Fatal("cancel retried")
	}
}
func TestRangesAndBatch(t *testing.T) {
	limit := errors.New("limit")
	seen := map[uint64]bool{}
	e := WalkRanges(context.Background(), 10, 1009, 1000, func(ctx context.Context, r Range) error {
		if r.To-r.From > 100 {
			return limit
		}
		for n := r.From; n <= r.To; n++ {
			if seen[n] {
				t.Fatal("overlap")
			}
			seen[n] = true
		}
		return nil
	}, func(e error) bool { return e == limit })
	if e != nil || len(seen) != 1000 {
		t.Fatal(e, len(seen))
	}
	out, e := MatchBatch([]uint64{1, 2}, []BatchResponse{{2, []byte("b")}, {1, []byte("a")}})
	if e != nil || string(out[0]) != "a" {
		t.Fatal(e)
	}
	if _, e = MatchBatch([]uint64{1, 2}, []BatchResponse{{1, nil}, {1, nil}}); e == nil {
		t.Fatal("duplicate")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if WalkRanges(ctx, 1, 2, 1, func(context.Context, Range) error { t.Fatal("canceled call"); return nil }, func(error) bool { return false }) != context.Canceled {
		t.Fatal("cancel")
	}
	if WalkRanges(context.Background(), 1, 1, 1, func(context.Context, Range) error { return limit }, func(error) bool { return true }) != limit {
		t.Fatal("blocked range skipped")
	}
}

func TestResponseByteBudget(t *testing.T) {
	body := &limitedBody{body: io.NopCloser(strings.NewReader("12345")), remaining: 4}
	b, e := io.ReadAll(body)
	if e == nil || string(b) != "1234" {
		t.Fatal(string(b), e)
	}
}
