package policy

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProductionParsingExactBounds(t *testing.T) {
	var dst struct {
		Values []int `json:"values"`
	}
	schema := MutationSchema{BodyBytes: 65536, Arrays: map[string]int{"/values": 2}}
	for _, body := range []string{`{"values":[1,2,3]}`, `{"values":[],"unknown":1}`} {
		if DecodeMutationSchema(httptest.NewRequest("POST", "/", strings.NewReader(body)), &dst, schema) == nil {
			t.Fatal("invalid schema accepted")
		}
	}
	err := DecodeMutationSchema(httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat(" ", 65537))), &dst, schema)
	if err == nil || MutationStatus(err) != 413 {
		t.Fatal("oversized mutation", err)
	}
	if err = DecodeMutationSchema(httptest.NewRequest("POST", "/", strings.NewReader(`{"values":[1,2]}`)), &dst, schema); err != nil {
		t.Fatal(err)
	}
	server := ProductionServer("127.0.0.1:0", http.NotFoundHandler())
	if server.ReadHeaderTimeout != 5*time.Second || server.IdleTimeout != 60*time.Second || server.MaxHeaderBytes != 16384 {
		t.Fatal("server bounds")
	}
	record := httptest.NewRecorder()
	RequestBounds(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("oversized query reached business handler") })).ServeHTTP(record, httptest.NewRequest("GET", "/?q="+strings.Repeat("a", 8193), nil))
	if record.Code != 414 {
		t.Fatal(record.Code)
	}
}

func TestRejectedMutationLeavesDestinationUnchanged(t *testing.T) {
	for _, body := range []string{`{"values":[1,2,3]}`, `{"values":[1],"unknown":true}`, `{"values":["invalid"]}`} {
		dst := struct {
			Values []int `json:"values"`
		}{Values: []int{99}}
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if DecodeMutationSchema(request, &dst, MutationSchema{BodyBytes: 65536, Arrays: map[string]int{"/values": 2}}) == nil {
			t.Fatal("invalid mutation accepted")
		}
		if len(dst.Values) != 1 || dst.Values[0] != 99 {
			t.Fatal("failed decode changed caller state", dst)
		}
	}
	var dst struct{ Name string }
	dst.Name = "preserved"
	if DecodeMutation(httptest.NewRequest("POST", "/", strings.NewReader(`{"Name":"changed","unknown":1}`)), &dst, 65536) == nil || dst.Name != "preserved" {
		t.Fatal("unknown field altered destination", dst)
	}
}

func TestOversizeHTTPMutationHasZeroDatabaseWrites(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	ctx := context.Background()
	table := pgx.Identifier{schema, "body_writes"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+table+"(payload text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			Payload string `json:"payload"`
		}
		if err := DecodeMutationSchema(r, &value, MutationSchema{BodyBytes: 65536}); err != nil {
			http.Error(w, "invalid body", MutationStatus(err))
			return
		}
		if _, err := pool.Exec(r.Context(), "INSERT INTO "+table+" VALUES($1)", value.Payload); err != nil {
			http.Error(w, "storage unavailable", 503)
			return
		}
		w.WriteHeader(204)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, size := range []int{65536, 65537} {
		body := `{"payload":"` + strings.Repeat("x", size-len(`{"payload":""}`)) + `"}`
		response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := 204
		if size > 65536 {
			want = 413
		}
		if response.StatusCode != want {
			t.Fatal(response.StatusCode, want)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
		t.Fatal("oversized request mutated database", count, err)
	}
}

func TestAcceptedMutationPreservesAbsentDefaults(t *testing.T) {
	dst := struct {
		Name   string `json:"name"`
		Values []int  `json:"values"`
	}{Name: "default", Values: []int{1}}
	if err := DecodeMutationSchema(httptest.NewRequest("PATCH", "/", strings.NewReader(`{"values":[2]}`)), &dst, MutationSchema{BodyBytes: 65536, Arrays: map[string]int{"/values": 2}}); err != nil {
		t.Fatal(err)
	}
	if dst.Name != "default" || len(dst.Values) != 1 || dst.Values[0] != 2 {
		t.Fatal("accepted patch changed absent defaults", dst)
	}
}
