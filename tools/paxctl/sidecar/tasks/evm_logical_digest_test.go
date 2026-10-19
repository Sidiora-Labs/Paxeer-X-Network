package tasks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"

	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
)

// digestRecordingUploader captures every put body keyed by S3 key.
type digestRecordingUploader struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (u *digestRecordingUploader) UploadObject(_ context.Context, in *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	var buf bytes.Buffer
	if in.Body != nil {
		if _, err := io.Copy(&buf, in.Body); err != nil {
			return nil, err
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.objects == nil {
		u.objects = map[string][]byte{}
	}
	u.objects[*in.Key] = buf.Bytes()
	return &transfermanager.UploadObjectOutput{}, nil
}

func (u *digestRecordingUploader) factory() seis3.UploaderFactory {
	return func(context.Context, string) (seis3.Uploader, error) { return u, nil }
}

func (u *digestRecordingUploader) record(t *testing.T, key string) EndpointDigestRecord {
	t.Helper()
	u.mu.Lock()
	raw, ok := u.objects[key]
	u.mu.Unlock()
	if !ok {
		t.Fatalf("no object published at key %q; have %v", key, u.keys())
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	dec := json.NewDecoder(gr)
	var rec EndpointDigestRecord
	if err := dec.Decode(&rec); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	return rec
}

func (u *digestRecordingUploader) keys() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	ks := make([]string, 0, len(u.objects))
	for k := range u.objects {
		ks = append(ks, k)
	}
	return ks
}

// fakeSeidb writes an executable shim that prints canned digest output. The
// shim branches on --backend and --memiavl-normalization and echoes the version
// it was asked for via --height so version-assertion paths can be exercised.
// flatkvVersionOverride / memiavlVersionOverride, when non-empty, replace the
// printed version for that backend's run to simulate a height mismatch (e.g. a
// snapshot tool clamping to the nearest available height).
func fakeSeidb(t *testing.T, flatkvVersionOverride, memiavlVersionOverride string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell shim not portable to windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "seidb")
	flatkvVer := `$height`
	if flatkvVersionOverride != "" {
		flatkvVer = flatkvVersionOverride
	}
	memiavlVer := `$height`
	if memiavlVersionOverride != "" {
		memiavlVer = memiavlVersionOverride
	}
	// The shim parses --backend, --height, --memiavl-normalization from argv and
	// prints a digest block whose values depend on backend+normalization.
	script := fmt.Sprintf(`#!/bin/sh
backend=""
height=""
norm=""
while [ $# -gt 0 ]; do
  case "$1" in
    --backend) backend="$2"; shift 2;;
    --height) height="$2"; shift 2;;
    --memiavl-normalization) norm="$2"; shift 2;;
    *) shift;;
  esac
done
acct=aaaa
code=cccc
stor=5555
leg=1111
if [ "$backend" = "memiavl" ] && [ "$norm" = "translator" ]; then
  stor=9999
fi
final=ffff
if [ "$backend" = "memiavl" ] && [ "$norm" = "translator" ]; then
  final=eeee
fi
if [ "$backend" = "memiavl" ]; then ver=%s; else ver=%s; fi
echo "EVM logical digest start"
echo "backend: $backend"
echo "version: $ver"
echo ""
echo "Bucket digests (final digest inputs)"
echo "account  count=10 bucket_digest=$acct"
echo "code     count=2 bucket_digest=$code"
echo "storage  count=99 bucket_digest=$stor"
echo "legacy   count=3 bucket_digest=$leg"
echo ""
echo "FINAL_DIGEST account+code+storage+legacy count=114 digest=$final"
`, memiavlVer, flatkvVer)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return path
}

func baseRequest(seidbPath string) EvmLogicalDigestRequest {
	return EvmLogicalDigestRequest{
		FlatKVDir:  "/data/flatkv",
		MemIAVLDir: "/data/memiavl",
		Height:     213200000,
		Bucket:     "digest-bucket",
		Prefix:     "digests",
		Region:     "us-east-1",
		SeidbPath:  seidbPath,
	}
}

func TestEvmLogicalDigest_SemanticMatchTranslatorMismatch(t *testing.T) {
	up := &digestRecordingUploader{}
	d := NewEvmLogicalDigester(up.factory())
	req := baseRequest(fakeSeidb(t, "", ""))

	if err := d.run(context.Background(), req); err != nil {
		t.Fatalf("run: %v", err)
	}

	// semantic: flatkv final ffff == memiavl final ffff -> match.
	sem := up.record(t, "digests/endpoint-digest-213200000-semantic.json.gz")
	if !sem.Match {
		t.Errorf("semantic should match, got %+v", sem)
	}
	if !sem.PerBucket["storage"].Match {
		t.Error("semantic storage should match")
	}

	// translator: memiavl storage/final diverge -> no match. This is the
	// fail-closed proof that a real divergence is reported, not masked.
	tr := up.record(t, "digests/endpoint-digest-213200000-translator.json.gz")
	if tr.Match {
		t.Errorf("translator should NOT match, got %+v", tr)
	}
	if tr.PerBucket["storage"].Match {
		t.Error("translator storage should not match")
	}
	if tr.PerBucket["account"].Match != true {
		t.Error("translator account should still match")
	}

	// axes_proved must never claim balance.
	for _, axis := range sem.AxesProved {
		if axis == "balance" {
			t.Fatal("axes_proved must not claim balance for the semantic path")
		}
	}
}

func TestEvmLogicalDigest_VersionMismatchFailsClosed(t *testing.T) {
	up := &digestRecordingUploader{}
	d := NewEvmLogicalDigester(up.factory())
	// flatkv shim prints version 999, but the request asks for 213200000.
	req := baseRequest(fakeSeidb(t, "999", ""))

	err := d.run(context.Background(), req)
	if err == nil {
		t.Fatal("expected version-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version, got: %v", err)
	}
	if len(up.keys()) != 0 {
		t.Errorf("nothing should be published on version mismatch, got %v", up.keys())
	}
}

func TestEvmLogicalDigest_MemiavlVersionMismatchFailsClosed(t *testing.T) {
	up := &digestRecordingUploader{}
	d := NewEvmLogicalDigester(up.factory())
	// flatkv opens at the requested height, but the memiavl snapshot resolves
	// to 888 (e.g. seidb clamped to the nearest available snapshot). The
	// comparison must not publish — a wrong-height memiavl side is a silent
	// false match in the degenerate case.
	req := baseRequest(fakeSeidb(t, "", "888"))

	err := d.run(context.Background(), req)
	if err == nil {
		t.Fatal("expected memiavl version-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "memiavl opened version") {
		t.Errorf("error should name the memiavl version mismatch, got: %v", err)
	}
	if len(up.keys()) != 0 {
		t.Errorf("nothing should be published on memiavl version mismatch, got %v", up.keys())
	}
}

func TestEvmLogicalDigest_Validation(t *testing.T) {
	d := NewEvmLogicalDigester((&digestRecordingUploader{}).factory())
	cases := map[string]func(*EvmLogicalDigestRequest){
		"missing bucket":      func(r *EvmLogicalDigestRequest) { r.Bucket = "" },
		"missing region":      func(r *EvmLogicalDigestRequest) { r.Region = "" },
		"missing flatkvDir":   func(r *EvmLogicalDigestRequest) { r.FlatKVDir = "" },
		"missing memiavlDir":  func(r *EvmLogicalDigestRequest) { r.MemIAVLDir = "" },
		"non-positive height": func(r *EvmLogicalDigestRequest) { r.Height = 0 },
		"bad normalization":   func(r *EvmLogicalDigestRequest) { r.Normalizations = []string{"bogus"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := baseRequest("seidb")
			mutate(&req)
			if err := d.run(context.Background(), req); err == nil {
				t.Fatalf("%s should fail validation", name)
			}
		})
	}
}

func TestEvmLogicalDigest_DefaultsApplied(t *testing.T) {
	req := EvmLogicalDigestRequest{
		FlatKVDir: "/a", MemIAVLDir: "/b", Height: 1, Bucket: "x", Region: "y",
	}
	if err := validateDigestRequest(&req); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if req.SeidbPath != defaultSeidbPath {
		t.Errorf("SeidbPath default = %q, want %q", req.SeidbPath, defaultSeidbPath)
	}
	if len(req.Normalizations) != 2 || req.Normalizations[0] != "semantic" || req.Normalizations[1] != "translator" {
		t.Errorf("Normalizations default = %v", req.Normalizations)
	}
}

func TestParseDigestOutput_HappyPath(t *testing.T) {
	out := strings.Join([]string{
		"EVM logical digest start",
		"backend: flatkv",
		"version: 213200000",
		"",
		"account  count=10 bucket_digest=aabb",
		"code     count=2 bucket_digest=ccdd",
		"storage  count=99 bucket_digest=eeff",
		"legacy   count=3 bucket_digest=1122",
		"",
		"FINAL_DIGEST account+code+storage+legacy count=114 digest=deadbeef",
	}, "\n")
	bd, err := parseDigestOutput(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if bd.Version != 213200000 {
		t.Errorf("version = %d", bd.Version)
	}
	if bd.Account != "aabb" || bd.Code != "ccdd" || bd.Storage != "eeff" || bd.Legacy != "1122" {
		t.Errorf("bucket digests = %+v", bd)
	}
	if bd.Final != "deadbeef" {
		t.Errorf("final = %q", bd.Final)
	}
}

func TestParseDigestOutput_FailClosed(t *testing.T) {
	cases := map[string]string{
		"missing final": strings.Join([]string{
			"version: 5",
			"account  count=1 bucket_digest=aa",
			"code     count=1 bucket_digest=bb",
			"storage  count=1 bucket_digest=cc",
			"legacy   count=1 bucket_digest=dd",
		}, "\n"),
		"missing version": strings.Join([]string{
			"account  count=1 bucket_digest=aa",
			"code     count=1 bucket_digest=bb",
			"storage  count=1 bucket_digest=cc",
			"legacy   count=1 bucket_digest=dd",
			"FINAL_DIGEST account+code+storage+legacy count=4 digest=ee",
		}, "\n"),
		"missing a bucket": strings.Join([]string{
			"version: 5",
			"account  count=1 bucket_digest=aa",
			"storage  count=1 bucket_digest=cc",
			"legacy   count=1 bucket_digest=dd",
			"FINAL_DIGEST account+code+storage+legacy count=3 digest=ee",
		}, "\n"),
		"empty": "",
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDigestOutput(out); err == nil {
				t.Fatalf("%s should be a parse error", name)
			}
		})
	}
}
