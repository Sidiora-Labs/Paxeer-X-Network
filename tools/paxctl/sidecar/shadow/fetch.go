package shadow

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
)

// FetchReport downloads and decodes a DivergenceReport from S3.
func FetchReport(ctx context.Context, downloader seis3.Downloader, bucket, key string) (*DivergenceReport, error) {
	resp, err := downloader.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("downloading s3://%s/%s: %w", bucket, key, err)
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if strings.HasSuffix(key, ".gz") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("decompressing report: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	var report DivergenceReport
	if err := json.NewDecoder(reader).Decode(&report); err != nil {
		return nil, fmt.Errorf("decoding report: %w", err)
	}

	return &report, nil
}
