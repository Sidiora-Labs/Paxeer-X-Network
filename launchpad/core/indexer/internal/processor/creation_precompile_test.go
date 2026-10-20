package processor

import (
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

func TestCreationAtPrecompile(t *testing.T) {
	const (
		factory = "0x00000000000000000000000000000000000000f1"
		token   = "0x00000000000000000000000000000000000000a1"
		pool    = "0x00000000000000000000000000000000000000b1"
		creator = "0x00000000000000000000000000000000000000c1"
		emitter = "0x00000000000000000000000000000000000000e1"
		tx      = "0x1111111111111111111111111111111111111111111111111111111111111111"
		block   = "0x2222222222222222222222222222222222222222222222222222222222222222"
	)
	precompile := strings.ToLower(deployment.LaunchpadPrecompileAddress)
	identity := decode.RawIdentity{ChainID: 125, BlockHash: block, TransactionHash: tx}
	market := &decode.DecodedEnvelope{Raw: decode.RawLog{Identity: identity, Emitter: factory, Order: decode.Order{Block: 42, Transaction: 3, Log: 0}}, ABIRevision: "factory-r1", EventName: "MarketCreated", EmitterRole: "SidioraFactory", Status: "decoded", Event: &decode.Event{EventName: "MarketCreated", Args: map[string]any{"token": token, "pool": pool, "creator": creator, "optical": "0", "nftId": "7"}}}
	mirror := func(role, at string) *decode.DecodedEnvelope {
		id := identity
		id.LogIndex = 1
		return &decode.DecodedEnvelope{Raw: decode.RawLog{Identity: id, Emitter: at, Order: decode.Order{Block: 42, Transaction: 3, Log: 1}}, ABIRevision: "emitter-r1", EventName: "TokenDeployed", EmitterRole: role, Status: "decoded", Event: &decode.Event{EventName: "TokenDeployed", Args: map[string]any{"token": token, "pool": pool, "creator": creator, "name": "Kindle", "symbol": "KDL", "decimals": "18", "totalSupply": "1000000000000000000000000000", "timestamp": "1700000000", "blockNumber": "42"}}}
	}
	provenance := map[string]accounting.Provenance{
		factory:    {DeploymentID: "factory", ABIRevision: "factory-r1", EmitterCodeEpoch: "1"},
		precompile: {DeploymentID: "precompile", ABIRevision: "emitter-r1", EmitterCodeEpoch: "1"},
		emitter:    {DeploymentID: "emitter", ABIRevision: "emitter-r1", EmitterCodeEpoch: "1"},
	}
	prove := func(a common.Address) (accounting.Provenance, error) {
		p, ok := provenance[strings.ToLower(a.Hex())]
		if !ok {
			t.Fatalf("provenance requested for unexpected emitter %s", a.Hex())
		}
		return p, nil
	}
	cases := []struct {
		name, role, at string
		processed      bool
	}{
		{"precompile", deployment.LaunchpadPrecompileRole, precompile, true},
		{"event-emitter", "EventEmitter", emitter, true},
		{"precompile-role-elsewhere", deployment.LaunchpadPrecompileRole, emitter, false},
		{"other-role-at-precompile", "SidioraFactory", precompile, false},
		{"other-role", "LaunchpadRouter", emitter, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts, err := creationFacts([]*decode.DecodedEnvelope{market, mirror(c.role, c.at)}, 1700000000, prove)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts) != 1 || facts[0].Token != token || facts[0].Pool != pool || facts[0].Creator != creator {
				t.Fatalf("creation not produced: %+v", facts)
			}
			m := facts[0].Metadata
			if !c.processed {
				if m != nil {
					t.Fatalf("mirror with role %q at %s accepted: %+v", c.role, c.at, m)
				}
				return
			}
			if m == nil || m.Emitter != c.at || m.Name != "Kindle" || m.Symbol != "KDL" || m.Decimals != 18 || m.Position.LogIndex != "1" || m.Provenance != provenance[c.at] {
				t.Fatalf("TokenDeployed mirror not processed: %+v", m)
			}
		})
	}
	bad := mirror(deployment.LaunchpadPrecompileRole, precompile)
	bad.Raw.Identity.BlockHash = "0x" + strings.Repeat("3", 64)
	if _, err := creationFacts([]*decode.DecodedEnvelope{market, bad}, 1700000000, prove); err == nil {
		t.Fatal("precompile mirror from another block accepted")
	}
}
