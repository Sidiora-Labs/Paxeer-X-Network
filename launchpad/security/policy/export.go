package policy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type AuditManifest struct {
	Version     int    `json:"version"`
	Day         string `json:"day"`
	Schema      string `json:"schema"`
	Rows        int64  `json:"rows"`
	First, Last int64
	SHA256      string    `json:"sha256"`
	KeyID       string    `json:"keyId"`
	RetainUntil time.Time `json:"retainUntil"`
}
type AuditExporter struct {
	Pool                          *pgxpool.Pool
	Schema, Bucket, Prefix, KeyID string
	Client                        *s3.Client
	Signer                        ed25519.PrivateKey
}

func (e AuditExporter) Export(ctx context.Context, day time.Time) (AuditManifest, error) {
	var manifest AuditManifest
	if e.Pool == nil || e.Client == nil || !safeIdentifier(e.Schema) || e.Bucket == "" || e.Prefix == "" || e.KeyID == "" || len(e.Signer) != ed25519.PrivateKeySize {
		return manifest, ErrDenied
	}
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	if end.After(time.Now()) {
		return manifest, ErrDenied
	}
	config, err := e.Client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{Bucket: aws.String(e.Bucket)})
	if err != nil || config.ObjectLockConfiguration == nil || config.ObjectLockConfiguration.ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		return manifest, errors.New("policy: retention authority unavailable")
	}
	rows, err := e.Pool.Query(ctx, "SELECT seq,at,record FROM "+pgx.Identifier{e.Schema, "audit_outbox"}.Sanitize()+" WHERE at>=$1 AND at<$2 ORDER BY seq", start, end)
	if err != nil {
		return manifest, err
	}
	defer rows.Close()
	var buf bytes.Buffer
	manifest = AuditManifest{Version: 1, Day: start.Format("2006-01-02"), Schema: e.Schema, KeyID: e.KeyID, RetainUntil: time.Now().UTC().Add(365 * 24 * time.Hour)}
	for rows.Next() {
		var seq int64
		var at time.Time
		var record json.RawMessage
		if err = rows.Scan(&seq, &at, &record); err != nil {
			return manifest, err
		}
		if manifest.Rows == 0 {
			manifest.First = seq
		}
		manifest.Last = seq
		manifest.Rows++
		if err = json.NewEncoder(&buf).Encode(struct {
			Sequence int64           `json:"sequence"`
			At       time.Time       `json:"at"`
			Record   json.RawMessage `json:"record"`
		}{seq, at, record}); err != nil {
			return manifest, err
		}
		if buf.Len() > 64<<20 {
			return manifest, errors.New("policy: audit export requires partitioning")
		}
	}
	if err = rows.Err(); err != nil {
		return manifest, err
	}
	sum := sha256.Sum256(buf.Bytes())
	manifest.SHA256 = hex.EncodeToString(sum[:])
	wire, err := json.Marshal(manifest)
	if err != nil {
		return manifest, err
	}
	signature := ed25519.Sign(e.Signer, wire)
	key := e.Prefix + "/" + manifest.Day + "/" + manifest.SHA256
	for _, artifact := range []struct {
		suffix string
		body   []byte
	}{{".jsonl", buf.Bytes()}, {".signature", signature}, {".manifest.json", wire}} {
		body := artifact.body
		object := key + artifact.suffix
		output, putErr := e.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(object), Body: bytes.NewReader(body), ObjectLockMode: types.ObjectLockModeCompliance, ObjectLockRetainUntilDate: &manifest.RetainUntil, ContentType: aws.String("application/octet-stream")})
		if putErr != nil {
			return manifest, errors.New("policy: retained audit upload failed")
		}
		retention, getErr := e.Client.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{Bucket: aws.String(e.Bucket), Key: aws.String(object), VersionId: output.VersionId})
		if getErr != nil || retention.Retention == nil || retention.Retention.Mode != types.ObjectLockRetentionModeCompliance || retention.Retention.RetainUntilDate == nil || retention.Retention.RetainUntilDate.Before(manifest.RetainUntil) {
			return manifest, errors.New("policy: audit retention not confirmed")
		}
	}
	return manifest, nil
}
