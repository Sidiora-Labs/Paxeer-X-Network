package policy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"
)

func TestSignedPublicReadLeaseCeilingAndExpiry(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	lease := EdgeLease{Origin: "origin-a", PolicyVersion: "v2", ID: "lease1", Issued: now, Expires: now.Add(time.Minute), PerSecond: 100}
	wire, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(key, wire)
	edge, err := NewEdgeAdmission(lease, signature, pub, "origin-a", now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if !edge.Take(now) {
			t.Fatal("signed ceiling prematurely exhausted", i)
		}
	}
	if edge.Take(now) || edge.Take(lease.Expires) {
		t.Fatal("lease ceiling or expiry bypass")
	}
	lease.PerSecond = 101
	wire, _ = json.Marshal(lease)
	if _, err = NewEdgeAdmission(lease, ed25519.Sign(key, wire), pub, "origin-a", now); err == nil {
		t.Fatal("oversized lease accepted")
	}
	lease.PerSecond = 100
	lease.Expires = now.Add(61 * time.Second)
	wire, _ = json.Marshal(lease)
	if _, err = NewEdgeAdmission(lease, ed25519.Sign(key, wire), pub, "origin-a", now); err == nil {
		t.Fatal("unbounded degradation admitted")
	}
}
