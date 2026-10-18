package policy

import (
	"context"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDeadLetterReplayRequiresRealAuthenticatedRequest(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	letters := DeadLetters{Pool: pool, DeliverySchema: schema, SecuritySchema: schema, Audience: "indexer", PolicyVersion: "v2"}
	if _, err := letters.RequestRedrive(context.Background(), nil, "stats", "g1", "hash", "operator reason"); err == nil {
		t.Fatal("redrive without authenticated request admitted")
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+schema+".deadletter_redrives").Scan(&count); err != nil || count != 0 {
		t.Fatal("unauthorized replay job", count, err)
	}
}

func TestRealDeadLetterDenialDuplicateReplayAndRedaction(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	ctx := context.Background()
	table := pgx.Identifier{schema, "delivery_quarantine"}.Sanitize()
	hash := strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, "CREATE TABLE "+table+"(consumer_id text NOT NULL,projection_generation text NOT NULL,envelope_hash text NOT NULL,failure_class text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),payload jsonb NOT NULL,PRIMARY KEY(consumer_id,projection_generation,envelope_hash))"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+table+"(consumer_id,projection_generation,envelope_hash,failure_class,payload) VALUES('stats','g1',$1,'secret-canary-error','{\"token\":\"secret-canary-payload\"}')", hash); err != nil {
		t.Fatal(err)
	}
	server, client, key := realPrivateOperatorServer(t, pool, schema, func(v WorkloadVerifier) http.Handler {
		d := DeadLetters{Pool: pool, DeliverySchema: schema, SecuritySchema: schema, Audience: "indexer", PolicyVersion: "v3", Verifier: v, Journal: PostgresJournal{Pool: pool, Schema: schema}}
		protected := d.Handler()
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/operator/redrive" {
				Authorize(v, d.Journal, "indexer", "replay", "v3", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					job, err := d.RequestRedrive(r.Context(), r, "stats", "g1", hash, "retry durable event")
					if err != nil {
						http.Error(w, "replay unavailable", 503)
						return
					}
					w.WriteHeader(202)
					_, _ = io.WriteString(w, job)
				})).ServeHTTP(w, r)
				return
			}
			protected.ServeHTTP(w, r)
		})
	})
	send := func(method, path, session string, want int) string {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-Actor-Role", "operator")
		if session != "" {
			signRealOperatorRequest(t, r, key, session)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatal(path, response.StatusCode, want, string(body))
		}
		return string(body)
	}
	send("POST", "/status/dead-letter/drain", "", 401)
	send("POST", "/operator/redrive", "ordinary-user", 403)
	var jobs, letters int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "deadletter_redrives"}.Sanitize()).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("user created replay job", jobs, err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "operator_sessions"}.Sanitize()+"(session_id,wallet,actor,audience,issued_at,expires_at,mfa_expires_at,scopes) VALUES('operator','0x52908400098527886E0F7030069857D2E4169EE7','operator-a','indexer',clock_timestamp()-interval '1 second',clock_timestamp()+interval '1 minute',clock_timestamp()+interval '1 minute',ARRAY['inspect','replay'])"); err != nil {
		t.Fatal(err)
	}
	body := send("GET", "/status/dead-letter?limit=1", "operator", 200)
	if strings.Contains(body, "secret-canary") || !strings.Contains(body, hash) {
		t.Fatal("inspection secret disclosure or missing durable identity", body)
	}
	job := send("POST", "/operator/redrive", "operator", 202)
	if duplicate := send("POST", "/operator/redrive", "operator", 202); duplicate != job {
		t.Fatal("duplicate replay identity changed", job, duplicate)
	}
	send("POST", "/status/dead-letter/drain", "operator", 403)
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "deadletter_redrives"}.Sanitize()).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("duplicate replay jobs", jobs, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&letters); err != nil || letters != 1 {
		t.Fatal("drain changed cardinality", letters, err)
	}
}
