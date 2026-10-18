package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DeadLetterMetadata struct {
	Consumer     string    `json:"consumer"`
	Generation   string    `json:"generation"`
	EnvelopeHash string    `json:"envelopeHash"`
	FailureClass string    `json:"failureClass"`
	Created      time.Time `json:"created"`
}
type DeadLetters struct {
	Pool                                                    *pgxpool.Pool
	DeliverySchema, SecuritySchema, Audience, PolicyVersion string
	Verifier                                                WorkloadVerifier
	Journal                                                 PostgresJournal
}

func (d DeadLetters) Inspect(ctx context.Context, after string, limit int) ([]DeadLetterMetadata, error) {
	if d.Pool == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(d.DeliverySchema) || limit < 1 || limit > 100 || len(after) > 2048 {
		return nil, ErrDenied
	}
	cursor := DeadLetterMetadata{}
	if after != "" {
		wire, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil {
			return nil, ErrDenied
		}
		decoder := json.NewDecoder(bytes.NewReader(wire))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || !digestPattern.MatchString(cursor.EnvelopeHash) || len(cursor.Consumer) > 128 || len(cursor.Generation) > 128 {
			return nil, ErrDenied
		}
	}
	rows, err := d.Pool.Query(ctx, "SELECT consumer_id,projection_generation,envelope_hash,failure_class,created_at FROM "+pgx.Identifier{d.DeliverySchema, "delivery_quarantine"}.Sanitize()+" WHERE (envelope_hash,consumer_id,projection_generation)>($1,$2,$3) ORDER BY envelope_hash,consumer_id,projection_generation LIMIT $4", cursor.EnvelopeHash, cursor.Consumer, cursor.Generation, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeadLetterMetadata{}
	for rows.Next() {
		var m DeadLetterMetadata
		if err = rows.Scan(&m.Consumer, &m.Generation, &m.EnvelopeHash, &m.FailureClass, &m.Created); err != nil {
			return nil, err
		}
		if !digestPattern.MatchString(m.EnvelopeHash) || len(m.Consumer) > 128 || len(m.Generation) > 128 {
			return nil, ErrDenied
		}
		switch m.FailureClass {
		case "invalid-envelope", "unsupported-version", "projection-conflict", "schema-conflict", "handler-error", "retry-exhausted", "authority-unavailable":
		default:
			m.FailureClass = "unclassified"
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (d DeadLetters) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status/dead-letter", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "invalid pagination", 400)
				return
			}
			limit = n
		}
		rows, err := d.Inspect(r.Context(), r.URL.Query().Get("after"), limit)
		if err != nil {
			http.Error(w, "inspection unavailable", 503)
			return
		}
		nextCursor := ""
		if len(rows) == limit {
			last := rows[len(rows)-1]
			wire, _ := json.Marshal(DeadLetterMetadata{Consumer: last.Consumer, Generation: last.Generation, EnvelopeHash: last.EnvelopeHash})
			nextCursor = base64.RawURLEncoding.EncodeToString(wire)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": rows, "nextCursor": nextCursor})
	})
	inspect := Authorize(d.Verifier, d.Journal, d.Audience, "inspect", d.PolicyVersion, mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status/dead-letter/drain" {
			Authorize(d.Verifier, d.Journal, d.Audience, "retention-delete", d.PolicyVersion, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "separate retention approval required", 403)
			})).ServeHTTP(w, r)
			return
		}
		inspect.ServeHTTP(w, r)
	})
}
func (d DeadLetters) RequestRedrive(ctx context.Context, request *http.Request, consumer, generation, envelopeHash, reason string) (string, error) {
	if request == nil {
		return "", ErrDenied
	}
	id, verifyErr := d.Verifier.Verify(ctx, request)
	if verifyErr != nil {
		return "", verifyErr
	}
	if d.Pool == nil || id.Kind != "operator" || !id.MFA || id.Audience != d.Audience || !id.Scopes["replay"] || !id.Expires.After(time.Now()) || id.Issued.After(time.Now()) || id.Expires.Sub(id.Issued) > 15*time.Minute || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(d.DeliverySchema) || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(d.SecuritySchema) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(envelopeHash) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`).MatchString(consumer) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`).MatchString(generation) || id.Actor == "" || d.PolicyVersion == "" || len(reason) < 1 || len(reason) > 1000 || strings.TrimSpace(reason) != reason || strings.ContainsAny(reason, "\r\n\x00") {
		return "", ErrDenied
	}
	wire, _ := json.Marshal([]string{consumer, generation, envelopeHash, reason, id.Actor})
	sum := sha256.Sum256(wire)
	job := hex.EncodeToString(sum[:])
	err := MutateAudited(ctx, d.Pool, pgx.Identifier{d.SecuritySchema, "audit_outbox"}, Audit{Actor: id.Actor, Action: "deadletter-redrive", Target: consumer + "/" + generation + "/" + envelopeHash, BeforeDigest: envelopeHash, AfterDigest: job, PolicyVersion: d.PolicyVersion}, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+pgx.Identifier{d.DeliverySchema, "delivery_quarantine"}.Sanitize()+" WHERE consumer_id=$1 AND projection_generation=$2 AND envelope_hash=$3)", consumer, generation, envelopeHash).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrDenied
		}
		_, err := tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{d.SecuritySchema, "deadletter_redrives"}.Sanitize()+"(job_id,consumer_id,projection_generation,envelope_hash,reason,actor) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(job_id) DO NOTHING", job, consumer, generation, envelopeHash, reason, id.Actor)
		return err
	})
	if err != nil {
		return "", err
	}
	return job, nil
}
