package storage

import (
	"context"
	"errors"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"net"
	"testing"
	"time"
)

func TestServePolicyReadOnlyApprovedPrefix(t *testing.T) {
	p, e := ScopedPolicy("owned-media-bucket", "serve")
	if e != nil {
		t.Fatal(e)
	}
	s := p.Statement[0]
	if len(s.Action) != 1 || s.Action[0] != "s3:GetObject" || len(s.Resource) != 1 || s.Resource[0] != "arn:aws:s3:::owned-media-bucket/approved/*" {
		t.Fatal(p)
	}
	for _, v := range []string{"*", "other/bucket", "BUCKET"} {
		if _, e := ScopedPolicy(v, "serve"); e == nil {
			t.Fatal(v)
		}
	}
	if _, e := ScopedPolicy("owned-media-bucket", "admin"); e == nil {
		t.Fatal("admin")
	}
}

func TestScannerRealConnectionFailureRetainsAlertTimeline(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if e = listener.Close(); e != nil {
		t.Fatal(e)
	}
	observer := NewMediaObserver()
	now := time.Now().UTC()
	observer.ScannerResult(true, 0, now)
	if e = observer.ProbeScanner(context.Background(), sharedhttp.ScanOptions{Host: "127.0.0.1", Port: port, Timeout: time.Second}, now); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	health, e := observer.Health(context.Background(), nil, now.Add(61*time.Second), true)
	if !errors.Is(e, ErrUnavailable) || !health.ScanAlert || health.ScannerUnavailableSince != now || health.WriteReady {
		t.Fatal(health, e)
	}
	if e = observer.RecordUpload("quarantined"); e != nil {
		t.Fatal(e)
	}
	observer.RecordRead(false)
	outcomes := observer.Snapshot()["uploadOutcomes"].(map[string]uint64)
	if outcomes["quarantined"] != 1 {
		t.Fatal(outcomes)
	}
}
