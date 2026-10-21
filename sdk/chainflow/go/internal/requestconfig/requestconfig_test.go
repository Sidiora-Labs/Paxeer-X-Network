package requestconfig_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"chainflow-data-gateway/internal/requestconfig"
	"chainflow-data-gateway/option"
)

type attempt struct {
	start    time.Time
	deadline time.Time
	ctxErr   error
}

type fakeTransport struct {
	mu        sync.Mutex
	attempts  []attempt
	responses []func(*http.Request) *http.Response
}

func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	deadline, _ := req.Context().Deadline()
	f.attempts = append(f.attempts, attempt{start: time.Now(), deadline: deadline, ctxErr: req.Context().Err()})
	n := len(f.attempts) - 1
	if n >= len(f.responses) {
		return nil, io.ErrUnexpectedEOF
	}
	return f.responses[n](req), nil
}

func response(req *http.Request, status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestRetryAfterLongerThanRequestTimeoutWaitsOnCallerContext(t *testing.T) {
	transport := &fakeTransport{
		responses: []func(*http.Request) *http.Response{
			func(req *http.Request) *http.Response {
				return response(req, http.StatusServiceUnavailable, http.Header{"Retry-After": {"2"}}, `{}`)
			},
			func(req *http.Request) *http.Response {
				return response(req, http.StatusOK, http.Header{"Content-Type": {"application/json"}}, `{"ok":true}`)
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out struct {
		OK bool `json:"ok"`
	}
	began := time.Now()
	err := requestconfig.ExecuteNewRequest(ctx, http.MethodGet, "status", nil, &out,
		option.WithBaseURL("http://fake.invalid/"),
		option.WithHTTPClient(transport),
		option.WithRequestTimeout(time.Second),
		option.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("expected the retried request to succeed, got %v", err)
	}
	if !out.OK {
		t.Fatalf("expected the second response body to be decoded")
	}
	if len(transport.attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(transport.attempts))
	}

	second := transport.attempts[1]
	if waited := second.start.Sub(began); waited < 2*time.Second {
		t.Fatalf("expected the second attempt to start after the 2s Retry-After delay, started after %v", waited)
	}
	if second.ctxErr != nil {
		t.Fatalf("expected the second attempt context to be live, got %v", second.ctxErr)
	}
	if second.deadline.IsZero() {
		t.Fatalf("expected the second attempt to carry its own request timeout deadline")
	}
	if remaining := second.deadline.Sub(second.start); remaining <= 0 || remaining > time.Second {
		t.Fatalf("expected the second attempt deadline to be derived when it starts (within 1s), got %v", remaining)
	}
}
