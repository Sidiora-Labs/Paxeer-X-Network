package capabilities

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
)

// TestLiveMainnetAttestation attests the shipped manifest against the real
// chain-125 RPC named by DEPLOYMENT_LIVE_RPC_URL. Skipped when unset.
func TestLiveMainnetAttestation(t *testing.T) {
	url := os.Getenv("DEPLOYMENT_LIVE_RPC_URL")
	if url == "" {
		t.Skip("DEPLOYMENT_LIVE_RPC_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := rpc.DialContext(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader, err := NewPinnedRPCReader(client)
	if err != nil {
		t.Fatal(err)
	}
	var latest struct {
		Hash common.Hash `json:"hash"`
	}
	if err = client.CallContext(ctx, &latest, "eth_getBlockByNumber", "latest", false); err != nil {
		t.Fatal(err)
	}
	m := deployment.Current()
	p := deployment.Policy{Now: time.Now().UTC(), MaxAge: 5 * time.Minute, Timeout: 30 * time.Second}
	a, err := deployment.Attest(ctx, reader, m, latest.Hash, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("block %d %s status=%s owner=%s pendingOwner=%s eligible=%v issues=%+v", a.BlockNumber, a.BlockHash, m.Status, a.ActualOwner, a.PendingOwner, a.Eligible, a.Issues)
	for _, issue := range a.Issues {
		if issue.Code == "manifest_governance_pending" || issue.Code == "governance_handoff_pending" {
			t.Fatalf("governance still pending: %+v", issue)
		}
	}
	if !a.Eligible {
		t.Fatalf("live deployment not eligible: %+v", a.Issues)
	}
	if err = a.ValidateFor(m, p); err != nil {
		t.Fatal(err)
	}
}
