package policy

import (
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestAttackEvidenceRequiresRawCapacityAndExactProfiles(t *testing.T) {
	profiles := FrozenAttackProfiles()
	if profiles["steady"].API != 10000 || profiles["steady"].Writes != 2000 || profiles["steady"].Quotes != 2000 || profiles["surge"].API != 20000 || profiles["surge"].Writes != 4000 || profiles["surge"].Quotes != 4000 {
		t.Fatal("frozen workload changed")
	}
	if ValidateAdversarialEvidence(profiles["steady"], AdversarialEvidence{}) == nil {
		t.Fatal("missing raw capacity evidence accepted")
	}
	e := AdversarialEvidence{ArtifactDigest: "not-a-digest", ConfigurationRevision: "v2", Started: time.Now(), Ended: time.Now().Add(time.Second), Replicas: 1, GeneratorRPS: 10000, CPUCores: 2, MemoryBytes: 1 << 30, Samples: []AdversarialSample{{Class: "account-write", Valid: false, Admitted: true, Status: 204}}}
	if ValidateAdversarialEvidence(profiles["steady"], e) == nil {
		t.Fatal("attack admitted or missing digest accepted")
	}
}

func TestAdversarialSampleRequiresRealAdmissionOutcome(t *testing.T) {
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: filepath.Join(t.TempDir(), "missing-redis.sock"), MaxRetries: -1, DialTimeout: time.Millisecond})
	defer client.Close()
	handler, err := Admission(AdmissionOptions{Redis: client, Namespace: "l45-phase3", Class: "public-read", Cost: 1}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	observed := time.Now()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	sample := AdversarialSample{Class: "public-read", RequestID: "request-observed", Observed: observed, Valid: true, Status: response.StatusCode, Duration: time.Since(observed), RedisUnavailable: true}
	if validateAdversarialSample(sample) != nil {
		t.Fatal("observed fail-closed admission rejected")
	}
	for _, change := range []string{"admitted", "success-without-admission", "zero-time", "negative-latency", "unknown-class"} {
		bad := sample
		switch change {
		case "admitted":
			bad.Admitted = true
		case "success-without-admission":
			bad.Status = 204
		case "zero-time":
			bad.Observed = time.Time{}
		case "negative-latency":
			bad.Authorization = -time.Nanosecond
		case "unknown-class":
			bad.Class = "unknown"
		}
		if validateAdversarialSample(bad) == nil {
			t.Fatal("invalid observed admission evidence", change)
		}
	}
	profile := FrozenAttackProfiles()["steady"]
	profile.API = 1
	if ValidateAdversarialEvidence(profile, AdversarialEvidence{}) == nil {
		t.Fatal("weakened capacity profile accepted")
	}
}
