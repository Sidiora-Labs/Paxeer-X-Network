// Package chainfixture replays recorded chain-125 (Paxeer) JSON-RPC traffic
// for tests. The fixture is the exact request/response set the production code
// paths issued against https://api5.mainnet-beta.paxeer.network for one real
// market (pool 0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263, created at block
// 33963716, traded at 33963733 and 33963749, fees claimed at 33963761). The
// node prunes blocks older than ~100k, so the traffic is recorded once with
// INDEXER_RECORD_PAXEER_RPC=<url> and replayed byte-for-byte afterwards; a call
// the fixture lacks fails the test. It is imported only by *_test.go files.
package chainfixture

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
)

// RecordEnv names the upstream RPC used to (re)record the fixture.
const RecordEnv = "INDEXER_RECORD_PAXEER_RPC"

const fixtureName = "paxeer125-market.jsonl.gz"

type rpcCall struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type recorded struct {
	Key    string          `json:"k"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// Key normalizes a call (method plus params with sorted object keys).
func Key(method string, params []json.RawMessage) string {
	normalized := make([]any, len(params))
	for i, p := range params {
		var v any
		if err := json.Unmarshal(p, &v); err != nil {
			normalized[i] = string(p)
			continue
		}
		normalized[i] = v
	}
	raw, _ := json.Marshal(normalized)
	return method + " " + string(raw)
}

// Fixture serves the recorded traffic over HTTP JSON-RPC.
type Fixture struct {
	// Mutate, when set, rewrites a recorded result before it is served.
	Mutate func(key string, result json.RawMessage) json.RawMessage

	mu       sync.Mutex
	entries  map[string]recorded
	upstream string
	path     string
	dirty    bool
	missing  []string
}

func fixturePath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", fixtureName)
}

// New loads the recorded fixture (or prepares to record when RecordEnv is set).
func New(t testing.TB) *Fixture {
	t.Helper()
	f := &Fixture{entries: map[string]recorded{}, upstream: os.Getenv(RecordEnv), path: fixturePath()}
	raw, err := os.Open(f.path)
	if err != nil {
		if f.upstream == "" {
			t.Fatalf("recorded chain fixture %s unavailable: %v", f.path, err)
		}
		return f
	}
	defer raw.Close()
	reader, err := gzip.NewReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<20), 64<<20)
	for scanner.Scan() {
		var r recorded
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		f.entries[r.Key] = r
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return f
}

// Recording reports whether calls are forwarded to a live upstream.
func (f *Fixture) Recording() bool { return f.upstream != "" }

// Serve starts the replay server; on cleanup it saves new recordings and fails
// the test for any call absent from the fixture.
func (f *Fixture) Serve(t testing.TB) string {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(func() {
		server.Close()
		if err := f.save(); err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.missing) > 0 {
			t.Errorf("unrecorded chain calls: %v", f.missing)
		}
	})
	return server.URL
}

func (f *Fixture) save() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.dirty {
		return nil
	}
	// Merge with entries other tests recorded since this fixture was loaded.
	if current, err := os.Open(f.path); err == nil {
		reader, gerr := gzip.NewReader(current)
		if gerr == nil {
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 64<<20), 64<<20)
			for scanner.Scan() {
				var r recorded
				if json.Unmarshal(scanner.Bytes(), &r) == nil {
					if _, ok := f.entries[r.Key]; !ok {
						f.entries[r.Key] = r
					}
				}
			}
		}
		current.Close()
	}
	keys := make([]string, 0, len(f.entries))
	for k := range f.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	for _, k := range keys {
		line, err := json.Marshal(f.entries[k])
		if err != nil {
			return err
		}
		if _, err = zw.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	f.dirty = false
	return os.WriteFile(f.path, buf.Bytes(), 0o644)
}

func (f *Fixture) answer(call rpcCall) (json.RawMessage, json.RawMessage, error) {
	key := Key(call.Method, call.Params)
	f.mu.Lock()
	entry, ok := f.entries[key]
	f.mu.Unlock()
	if !ok {
		if f.upstream == "" {
			f.mu.Lock()
			f.missing = append(f.missing, key)
			f.mu.Unlock()
			return nil, nil, errors.New("unrecorded call " + key)
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": call.Method, "params": call.Params})
		response, err := http.Post(f.upstream, "application/json", bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		defer response.Body.Close()
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err = json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			return nil, nil, err
		}
		entry = recorded{Key: key, Result: envelope.Result, Error: envelope.Error}
		f.mu.Lock()
		f.entries[key] = entry
		f.dirty = true
		f.mu.Unlock()
	}
	result := entry.Result
	if f.Mutate != nil && len(entry.Error) == 0 {
		result = f.Mutate(key, result)
	}
	return result, entry.Error, nil
}

// ServeHTTP answers one JSON-RPC call from the recording.
func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var call rpcCall
	if err = json.Unmarshal(body, &call); err != nil {
		http.Error(w, "batch requests are not recorded", http.StatusBadRequest)
		return
	}
	result, rpcErr, err := f.answer(call)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	if len(rpcErr) > 0 {
		out["error"] = rpcErr
	} else {
		out["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
