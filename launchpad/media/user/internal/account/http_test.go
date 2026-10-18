package account

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func httpRequest(handler http.Handler, p Principal, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if p.AccountID != uuid.Nil {
		request = request.WithContext(WithPrincipal(request.Context(), p))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestPostgresHTTPFrozenProfile(t *testing.T) {
	s := fixture(t)
	p := owner(t, s, 800)
	other := owner(t, s, 801)
	handler := (HTTP{Store: s}).Handler()
	wallet := fmt.Sprintf("0x%040x", 800)
	path := "/v1/profiles/" + wallet
	body := `{"expectedRevision":"0","displayName":"<script>payload</script>","bio":"plain","avatarAssetId":null}`
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- httpRequest(handler, p, "PATCH", path, body) }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for result := range results {
		if result.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(result.Header())
		}
		switch result.Code {
		case 200:
			success++
		case 412:
			conflict++
			var wire WireError
			must(t, json.Unmarshal(result.Body.Bytes(), &wire))
			if wire.Code != "REVISION_CONFLICT" {
				t.Fatal(wire)
			}
		default:
			t.Fatal(result.Code, result.Body.String())
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	if result := httpRequest(handler, other, "PATCH", path, `{"expectedRevision":"1","bio":"stolen"}`); result.Code != 403 {
		t.Fatal(result.Code, result.Body.String())
	}
	missing := httptest.NewRequest("PATCH", path, strings.NewReader(`{"expectedRevision":"1","bio":"stolen"}`))
	missing.Header.Set("X-Actor-Wallet", wallet)
	missing.Header.Set("Authorization", "Bearer forged")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != 401 {
		t.Fatal(response.Code, response.Body.String())
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	public, err := server.Client().Get(server.URL + path)
	must(t, err)
	defer public.Body.Close()
	raw, err := io.ReadAll(public.Body)
	must(t, err)
	if public.StatusCode != 200 || public.Header.Get("Cache-Control") != "public, max-age=1, stale-while-revalidate=2" || public.Header.Get("ETag") == "" {
		t.Fatal(public.StatusCode, public.Header, string(raw))
	}
	var envelope struct {
		Data struct {
			Profile map[string]json.RawMessage `json:"profile"`
		} `json:"data"`
		Revision string `json:"revision"`
	}
	must(t, json.Unmarshal(raw, &envelope))
	expected := []string{"wallet", "displayName", "bio", "avatarAssetId", "bannerAssetId", "website", "revision"}
	if len(envelope.Data.Profile) != len(expected) {
		t.Fatal(string(raw))
	}
	for _, key := range expected {
		if _, ok := envelope.Data.Profile[key]; !ok {
			t.Fatal(key, string(raw))
		}
	}
	if envelope.Revision != "1" || bytes.Contains(raw, []byte("<script>")) || !bytes.Contains(raw, []byte(`\u003cscript\u003e`)) {
		t.Fatal(string(raw))
	}
	for _, bad := range []string{`{"expectedRevision":"1","bio":null}`, `{"expectedRevision":"1","unknown":true}`, strings.Repeat(" ", 16385)} {
		rejected := httpRequest(handler, p, "PATCH", path, bad)
		if rejected.Code != 422 {
			t.Fatal(rejected.Code, rejected.Body.String())
		}
	}
	var revision int64
	must(t, s.DB.QueryRow(context.Background(), `SELECT revision FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&revision))
	if revision != 1 {
		t.Fatal("rejected request mutated profile", revision)
	}
}

func TestPostgresHTTPFrozenWatchlist(t *testing.T) {
	s := fixture(t)
	p := owner(t, s, 810)
	other := owner(t, s, 811)
	_, err := s.DB.Exec(context.Background(), `INSERT INTO account_watch SELECT $1,125,'0x'||lpad(to_hex(i),40,'0'),$2,0,false FROM generate_series(1,500) i`, p.AccountID, s.now())
	must(t, err)
	handler := (HTTP{Store: s, CursorKey: bytes.Repeat([]byte{7}, 32)}).Handler()
	var cursor *string
	count := 0
	seen := map[string]bool{}
	var firstCursor string
	for {
		path := "/v1/me/watchlist?limit=100"
		if cursor != nil {
			path += "&cursor=" + url.QueryEscape(*cursor)
		}
		response := httpRequest(handler, p, "GET", path, "")
		if response.Code != 200 || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(response.Code, response.Body.String())
		}
		var envelope struct {
			Data struct {
				Items []WireWatchEntry `json:"items"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		must(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		for _, entry := range envelope.Data.Items {
			if seen[entry.PoolAddress] || entry.Order != count || entry.Order > 499 || entry.Wallet != fmt.Sprintf("0x%040x", 810) {
				t.Fatal(entry, count)
			}
			seen[entry.PoolAddress] = true
			count++
		}
		cursor = envelope.NextCursor
		if cursor == nil {
			break
		}
		if len(*cursor) > 256 {
			t.Fatal("cursor violates frozen bound", len(*cursor))
		}
		if firstCursor == "" {
			firstCursor = *cursor
		}
	}
	if count != 500 {
		t.Fatal(count)
	}
	for _, test := range []struct {
		principal Principal
		path      string
		code      int
	}{
		{other, "/v1/me/watchlist?cursor=" + url.QueryEscape(firstCursor), 400},
		{p, "/v1/me/watchlist?cursor=" + url.QueryEscape(firstCursor[:len(firstCursor)-1]+"!"), 400},
		{p, "/v1/me/watchlist?limit=101", 422},
		{p, "/v1/me/watchlist?limit=1&limit=2", 422},
		{p, "/v1/me/watchlist?accountId=" + other.AccountID.String(), 422},
	} {
		response := httpRequest(handler, test.principal, "GET", test.path, "")
		if response.Code != test.code {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	pool := "0x0000000000000000000000000000000000000001"
	missingDependency := httpRequest(handler, p, "PUT", "/v1/me/watchlist/"+pool, `{"alertEnabled":true}`)
	if missingDependency.Code != 503 {
		t.Fatal(missingDependency.Code, missingDependency.Body.String())
	}
	deletion := httpRequest(handler, other, "DELETE", "/v1/me/watchlist/"+pool, "")
	if deletion.Code != 204 {
		t.Fatal(deletion.Code, deletion.Body.String())
	}
	var retained int
	must(t, s.DB.QueryRow(context.Background(), `SELECT count(*) FROM account_watch WHERE account_id=$1 AND pool=$2`, p.AccountID, pool).Scan(&retained))
	if retained != 1 {
		t.Fatal("other owner deleted entry")
	}
	deletion = httpRequest(handler, p, "DELETE", "/v1/me/watchlist/"+pool, "")
	if deletion.Code != 204 {
		t.Fatal(deletion.Code, deletion.Body.String())
	}
	deletion = httpRequest(handler, p, "DELETE", "/v1/me/watchlist/"+pool, "")
	if deletion.Code != 204 {
		t.Fatal(deletion.Code, deletion.Body.String())
	}
	stale := httpRequest(handler, p, "GET", "/v1/me/watchlist?cursor="+url.QueryEscape(firstCursor), "")
	if stale.Code != 409 {
		t.Fatal(stale.Code, stale.Body.String())
	}
	var wire WireError
	must(t, json.Unmarshal(stale.Body.Bytes(), &wire))
	if wire.Code != "RESNAPSHOT_REQUIRED" {
		t.Fatal(wire)
	}
}
