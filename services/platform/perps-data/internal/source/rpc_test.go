package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
)

// observer passes every request to the node and records its URL and method;
// a method in replay is answered with that recorded node response instead.
type observer struct {
	mu     sync.Mutex
	seen   []observed
	replay map[string][]byte
}

type observed struct{ url, method string }

func (o *observer) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	o.mu.Lock()
	o.seen = append(o.seen, observed{r.URL.String(), req.Method})
	rec, ok := o.replay[req.Method]
	o.mu.Unlock()
	if ok {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(bytes.NewReader(rec)), ContentLength: int64(len(rec)), Request: r}, nil
	}
	out := r.Clone(r.Context())
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	return http.DefaultTransport.RoundTrip(out)
}

func (o *observer) take() []observed {
	o.mu.Lock()
	defer o.mu.Unlock()
	seen := o.seen
	o.seen = nil
	return seen
}

func testRPCURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PERPS_DATA_TEST_RPC")
	if u == "" {
		t.Fatal("PERPS_DATA_TEST_RPC is required: the revert classification runs against the node's own responses")
	}
	return u
}

// twoSlotPool is a pool of two slots of the node at url, told apart by the
// fragment, which is not sent.
func twoSlotPool(t *testing.T, url string, obs *observer) *RPC {
	t.Helper()
	s, err := NewRPC(context.Background(), RPCOptions{URLs: []string{url, url + "#b"}, HealthInterval: time.Hour, httpClient: &http.Client{Transport: obs}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	obs.take()
	return s
}

func failures(s *RPC, url string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range s.slots {
		if sl.url == url {
			return sl.consecutiveFailures
		}
	}
	return -1
}

func perpsCall(t *testing.T, a abi.ABI, method string) []byte {
	t.Helper()
	data, err := a.Pack(method, new(big.Int).Lsh(big.NewInt(1), 200))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRPCRevertRealNode(t *testing.T) {
	url := testRPCURL(t)
	a, err := perpsabi.ABI()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, unknown, selector string }{
		{"getPosition", "PerpsUnknownPosition", "6dcd3d95"},
		{"getOrder", "PerpsUnknownOrder", "1930eff6"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			if got := common.Bytes2Hex(a.Errors[tc.unknown].ID.Bytes()[:4]); got != tc.selector {
				t.Fatalf("%s selector %s, want %s", tc.unknown, got, tc.selector)
			}
			obs := &observer{}
			s := twoSlotPool(t, url, obs)
			first := s.slotURL(s.rankedIndices()[0])

			_, err := s.Call(context.Background(), PerpsModule, perpsCall(t, a, tc.method))
			var revert *decode.RevertError
			if !errors.As(err, &revert) {
				t.Fatalf("%s of an unknown id: %v, want a revert", tc.method, err)
			}
			if common.Bytes2Hex(revert.Data) != tc.selector {
				t.Fatalf("revert data %x, want %s", revert.Data, tc.selector)
			}
			seen := obs.take()
			if len(seen) != 1 || seen[0].url != first || seen[0].method != "eth_call" {
				t.Fatalf("revert asked %v, want one eth_call to %s", seen, first)
			}
			if f := failures(s, first); f != 0 {
				t.Fatalf("revert marked %d failure(s) on the node", f)
			}
			if len(s.rankedIndices()) != 2 || s.slotURL(s.rankedIndices()[0]) != first {
				t.Fatalf("node %s no longer first among the healthy nodes", first)
			}

			if _, err := s.Head(context.Background()); err != nil {
				t.Fatal(err)
			}
			if seen := obs.take(); len(seen) != 1 || seen[0].url != first {
				t.Fatalf("following call asked %v, want the same node %s", seen, first)
			}
		})
	}
}

// postRaw sends body to the node and returns its raw response.
func postRaw(t *testing.T, url, body string) []byte {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type rpcErrorShape struct {
	Error map[string]json.RawMessage `json:"error"`
}

func errorShape(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var r rpcErrorShape
	if err := json.Unmarshal(raw, &r); err != nil || r.Error == nil {
		t.Fatalf("node response %s carries no error object", raw)
	}
	return r.Error
}

func TestRPCRevertShapes(t *testing.T) {
	url := testRPCURL(t)
	a, err := perpsabi.ABI()
	if err != nil {
		t.Fatal(err)
	}
	call := func(data string) string {
		return `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"to":"` + PerpsModule.Hex() + `","data":"` + data + `"},"latest"]}`
	}

	// Record once from the node and pin the recorded shape.
	withData := postRaw(t, url, call("0x"+common.Bytes2Hex(perpsCall(t, a, "getPosition"))))
	e := errorShape(t, withData)
	if string(e["code"]) != "3" || string(e["data"]) != `"0x6dcd3d95"` || string(e["message"]) != `"execution reverted"` {
		t.Fatalf("recorded revert with data %s, want code 3, message execution reverted, data 0x6dcd3d95", withData)
	}
	noData := postRaw(t, url, call("0x"))
	if e := errorShape(t, noData); string(e["code"]) != "-32000" || string(e["message"]) != `"execution reverted"` || e["data"] != nil {
		t.Fatalf("recorded revert without data %s, want code -32000, message execution reverted, no data", noData)
	}
	// Code 3 with a message only: the recorded code 3 response without data.
	var messageOnly map[string]any
	if err := json.Unmarshal(withData, &messageOnly); err != nil {
		t.Fatal(err)
	}
	delete(messageOnly["error"].(map[string]any), "data")
	code3Message, err := json.Marshal(messageOnly)
	if err != nil {
		t.Fatal(err)
	}
	notServed := postRaw(t, url, `{"jsonrpc":"2.0","id":1,"method":"perps_unknownMethod","params":[]}`)
	if e := errorShape(t, notServed); string(e["code"]) != "-32601" {
		t.Fatalf("recorded unknown method %s, want code -32601", notServed)
	}

	for _, tc := range []struct {
		name     string
		recorded []byte
		revert   bool
		data     string
	}{
		{"data as 0x-hex string", withData, true, "6dcd3d95"},
		{"data absent", noData, true, ""},
		{"code 3 with a message only", code3Message, true, ""},
		{"non-code-3 error", notServed, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obs := &observer{replay: map[string][]byte{"eth_call": tc.recorded}}
			s := twoSlotPool(t, url, obs)
			order := s.rankedIndices()
			_, err := s.Call(context.Background(), PerpsModule, []byte{0})
			seen := obs.take()
			var revert *decode.RevertError
			if tc.revert {
				if !errors.As(err, &revert) || common.Bytes2Hex(revert.Data) != tc.data {
					t.Fatalf("classified %v, want a revert with data %q", err, tc.data)
				}
				if len(seen) != 1 || seen[0].url != s.slotURL(order[0]) {
					t.Fatalf("revert asked %v, want the first node only", seen)
				}
				for _, idx := range order {
					if f := failures(s, s.slotURL(idx)); f != 0 {
						t.Fatalf("revert marked %d failure(s) on %s", f, s.slotURL(idx))
					}
				}
				return
			}
			if err == nil || errors.As(err, &revert) || errors.Is(err, decode.ErrStatePruned) || errors.Is(err, ErrResponseSize) {
				t.Fatalf("classified %v, want a node failure", err)
			}
			if len(seen) != 2 {
				t.Fatalf("node failure asked %v, want both nodes", seen)
			}
			for _, idx := range order {
				if f := failures(s, s.slotURL(idx)); f != 1 {
					t.Fatalf("node failure marked %d failure(s) on %s, want 1", f, s.slotURL(idx))
				}
			}
		})
	}
}
