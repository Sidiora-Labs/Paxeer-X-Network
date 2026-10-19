package s3

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
)

// captureUploader records the put input and drains the streamed body into a
// buffer, mimicking S3 reading the gzip stream off the pipe.
type captureUploader struct {
	in   *transfermanager.UploadObjectInput
	body []byte
	err  error
}

func (u *captureUploader) UploadObject(_ context.Context, in *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	u.in = in
	if u.err != nil {
		// Don't drain — exercise the early-abort path where the writer
		// goroutine must be unblocked via CloseWithError.
		return nil, u.err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, in.Body); err != nil {
		return nil, err
	}
	u.body = buf.Bytes()
	return &transfermanager.UploadObjectOutput{}, nil
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading gzip: %v", err)
	}
	return out
}

func TestStreamGzipNDJSON_RoundTripAndSeal(t *testing.T) {
	up := &captureUploader{}
	records := []map[string]any{
		{"height": 1, "ok": true},
		{"height": 2, "ok": false},
	}

	res, err := StreamGzipNDJSON(context.Background(), up, "bkt", "k.ndjson.gz", records)
	if err != nil {
		t.Fatalf("StreamGzipNDJSON: %v", err)
	}

	payload := gunzip(t, up.body)
	lines := strings.Split(strings.TrimRight(string(payload), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d NDJSON lines, want 2: %q", len(lines), payload)
	}

	// The returned seal must equal SHA-256 over the uncompressed payload.
	want := sha256.Sum256(payload)
	if res.UncompressedSHA256 != hex.EncodeToString(want[:]) {
		t.Errorf("UncompressedSHA256 = %s, want %s", res.UncompressedSHA256, hex.EncodeToString(want[:]))
	}

	// The put must request a SHA-256 checksum (the wire seal over stored bytes).
	if up.in.ChecksumAlgorithm != tmtypes.ChecksumAlgorithmSha256 {
		t.Errorf("ChecksumAlgorithm = %q, want SHA256", up.in.ChecksumAlgorithm)
	}
}

func TestStreamGzipJSON_RoundTripAndSeal(t *testing.T) {
	up := &captureUploader{}
	obj := map[string]any{"match": true, "height": 42}

	res, err := StreamGzipJSON(context.Background(), up, "bkt", "k.json.gz", obj)
	if err != nil {
		t.Fatalf("StreamGzipJSON: %v", err)
	}

	payload := gunzip(t, up.body)
	want := sha256.Sum256(payload)
	if res.UncompressedSHA256 != hex.EncodeToString(want[:]) {
		t.Errorf("UncompressedSHA256 = %s, want %s", res.UncompressedSHA256, hex.EncodeToString(want[:]))
	}
	if !strings.Contains(string(payload), "\"match\": true") {
		t.Errorf("payload not indented JSON: %q", payload)
	}
}

func TestStreamGzipFunc_LazyWriter(t *testing.T) {
	up := &captureUploader{}
	res, err := StreamGzipFunc(context.Background(), up, "bkt", "k", func(w io.Writer) error {
		_, err := io.WriteString(w, "line-a\nline-b\n")
		return err
	})
	if err != nil {
		t.Fatalf("StreamGzipFunc: %v", err)
	}
	payload := gunzip(t, up.body)
	if string(payload) != "line-a\nline-b\n" {
		t.Errorf("payload = %q", payload)
	}
	want := sha256.Sum256(payload)
	if res.UncompressedSHA256 != hex.EncodeToString(want[:]) {
		t.Errorf("seal mismatch")
	}
}

func TestStreamGzip_WriterErrorPropagates(t *testing.T) {
	up := &captureUploader{}
	sentinel := errors.New("boom")
	_, err := StreamGzipFunc(context.Background(), up, "bkt", "k", func(io.Writer) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrap of sentinel", err)
	}
}

func TestStreamGzip_WriterPanicBecomesError(t *testing.T) {
	// A panic in the payload writer (e.g. a record's MarshalJSON over
	// attacker-adjacent chain data) runs on a task-spawned goroutine outside the
	// engine's handler recover. It must convert to a returned error — not crash
	// the sidecar. Reaching this assertion at all proves the process survived.
	up := &captureUploader{}
	_, err := StreamGzipFunc(context.Background(), up, "bkt", "k", func(io.Writer) error {
		panic("marshal blew up")
	})
	if err == nil {
		t.Fatal("expected an error from a panicking writer, got nil")
	}
	if !strings.Contains(err.Error(), "panic in payload writer") {
		t.Fatalf("err = %v, want it to name the recovered panic", err)
	}
}

func TestStreamGzip_UploadErrorUnblocksWriter(t *testing.T) {
	// A writer large enough to fill the pipe and gzip buffers would deadlock if
	// the pipe were never closed on upload failure. Reaching the return without
	// hanging proves CloseWithError unblocked it (go test's timeout is the
	// backstop if it does not).
	up := &captureUploader{err: errors.New("s3 down")}
	_, err := StreamGzipFunc(context.Background(), up, "bkt", "k", func(w io.Writer) error {
		for i := 0; i < 100000; i++ {
			if _, werr := io.WriteString(w, "padding-line\n"); werr != nil {
				return werr
			}
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected upload error, got nil")
	}
}
