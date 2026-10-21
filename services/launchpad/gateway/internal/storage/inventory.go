package storage

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	sharedstorage "github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type InventoryReport struct {
	Checked     int       `json:"checked"`
	Orphans     []string  `json:"orphans"`
	Truncated   bool      `json:"truncated"`
	GeneratedAt time.Time `json:"generatedAt"`
}

func (a *Authority) ReconcileInventory(ctx context.Context, client *s3.Client, bucket, prefix string, limit int) (InventoryReport, error) {
	report := InventoryReport{Orphans: []string{}, GeneratedAt: time.Now().UTC()}
	if client == nil || bucket == "" || limit < 1 || limit > 100000 || (prefix != "approved/" && prefix != "private/" && prefix != "quarantine/" && prefix != "temporary/") {
		return report, ErrConflict
	}
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(1000)})
	for pages.HasMorePages() {
		page, e := pages.NextPage(ctx)
		if e != nil {
			return report, ErrUnavailable
		}
		for _, object := range page.Contents {
			if report.Checked >= limit {
				report.Truncated = true
				return report, nil
			}
			key := aws.ToString(object.Key)
			report.Checked++
			if !strings.HasPrefix(key, prefix) {
				return report, ErrConflict
			}
			var hash, kind string
			query := "SELECT hash,kind FROM " + a.table("lineage") + " WHERE object_key=$1 AND removed_at IS NULL ORDER BY hash LIMIT 1"
			e = a.Pool.QueryRow(ctx, query, key).Scan(&hash, &kind)
			if errors.Is(e, pgx.ErrNoRows) {
				report.Orphans = append(report.Orphans, key)
				continue
			}
			if e != nil {
				return report, ErrUnavailable
			}
			if kind == "variant" || kind == "original" {
				expected := hash
				if kind == "variant" {
					if !ValidDeliveryKey(key) {
						return report, ErrConflict
					}
					expected = strings.TrimSuffix(strings.Split(key, "/")[3], ".png")
				}
				bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
				e = verifyInventoryHash(bounded, client, bucket, key, expected)
				cancel()
				if e != nil {
					return report, e
				}
			}
		}
	}
	return report, nil
}

func (a *Authority) InventoryReportSchemaDDL() string {
	return "CREATE TABLE " + a.table("inventory_reports") + "(id uuid PRIMARY KEY,bucket text NOT NULL,prefix text NOT NULL,checked integer NOT NULL CHECK(checked>=0),orphans jsonb NOT NULL,truncated boolean NOT NULL,generated_at timestamptz NOT NULL);"
}

func NewScopedObjectClient(ctx context.Context, config sharedstorage.Config) (*sharedstorage.Client, *s3.Client, error) {
	endpoint, e := url.Parse(config.Endpoint)
	if e != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || config.Bucket == "" || config.Region == "" || config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return nil, nil, ErrUnavailable
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.TLSHandshakeTimeout = 2 * time.Second
	transport.ResponseHeaderTimeout = 3 * time.Second
	transport.MaxConnsPerHost = 4
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cfg, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(config.Region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")), awsconfig.WithHTTPClient(client))
	if e != nil {
		return nil, nil, ErrUnavailable
	}
	raw := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(config.Endpoint)
		o.UsePathStyle = true
		o.RetryMaxAttempts = 2
	})
	return sharedstorage.NewFromS3(raw, config.Bucket), raw, nil
}
func (a *Authority) PersistInventory(ctx context.Context, bucket, prefix string, report InventoryReport) error {
	if bucket == "" || (prefix != "approved/" && prefix != "private/" && prefix != "temporary/" && prefix != "quarantine/") || report.Checked < 0 || report.Checked > 100000 || len(report.Orphans) > 100000 || report.GeneratedAt.IsZero() {
		return ErrConflict
	}
	raw, e := json.Marshal(report.Orphans)
	if e != nil {
		return ErrConflict
	}
	if _, e = a.Pool.Exec(ctx, "INSERT INTO "+a.table("inventory_reports")+"(id,bucket,prefix,checked,orphans,truncated,generated_at) VALUES($1,$2,$3,$4,$5,$6,$7)", uuid.NewString(), bucket, prefix, report.Checked, raw, report.Truncated, report.GeneratedAt); e != nil {
		return ErrUnavailable
	}
	return nil
}

func verifyInventoryHash(ctx context.Context, client *s3.Client, bucket, key, expected string) error {
	object, e := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if e != nil {
		return ErrUnavailable
	}
	if object == nil || object.Body == nil {
		return ErrUnavailable
	}
	defer object.Body.Close()
	size := aws.ToInt64(object.ContentLength)
	if size < 1 || size > 32<<20 || !digest.MatchString(expected) {
		return ErrConflict
	}
	hash := sha256.New()
	read, e := io.Copy(hash, io.LimitReader(object.Body, (32<<20)+1))
	if e != nil {
		return ErrUnavailable
	}
	if read != size || fmt.Sprintf("%x", hash.Sum(nil)) != expected {
		return ErrConflict
	}
	return nil
}
