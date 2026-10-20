package receipt

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

func TestValidateCodePrecompile(t *testing.T) {
	manifest := deployment.Current()
	identity, ok := manifest.Roles[deployment.LaunchpadPrecompileRole]
	if !ok || identity.Kind != "precompile" || identity.Address != deployment.LaunchpadPrecompileAddress {
		t.Fatalf("manifest precompile role %+v", identity)
	}
	hash, err := manifest.Hash()
	if err != nil {
		t.Fatal(err)
	}
	// No client: the precompile identity is qualified without a code read.
	a := &RPCQualification{options: RPCOptions{Manifest: manifest}, manifestHash: hash}
	address := common.HexToAddress(deployment.LaunchpadPrecompileAddress)
	epoch := RPCCodeEpoch{Deployment: decode.Deployment{Address: address, Role: deployment.LaunchpadPrecompileRole, From: decode.Order{Block: manifest.ActivationHeight}}, Identity: identity}
	digest, err := a.validateCodeUncached(context.Background(), epoch, common.Hash{})
	if err != nil || digest == "" {
		t.Fatalf("precompile epoch rejected: %q %v", digest, err)
	}
	if !validRPCIdentity(identity) {
		t.Fatal("precompile identity invalid")
	}
	other := common.HexToAddress("0x0000000000000000000000000000000000001018")
	for name, mutate := range map[string]func(*RPCCodeEpoch){
		"deployment address": func(e *RPCCodeEpoch) { e.Deployment.Address = other },
		"identity address":   func(e *RPCCodeEpoch) { e.Identity.Address = other.Hex() },
		"both addresses":     func(e *RPCCodeEpoch) { e.Deployment.Address = other; e.Identity.Address = other.Hex() },
		"role":               func(e *RPCCodeEpoch) { e.Deployment.Role = "EventEmitter" },
		"runtime hash": func(e *RPCCodeEpoch) {
			e.Identity.RuntimeHash = "0x6302ff80f4168c2af23afbf3117c613edd66d288d120eee825f9ecadd43a392f"
		},
		"implementation": func(e *RPCCodeEpoch) { e.Identity.ImplementationAddress = other.Hex() },
		"beacon":         func(e *RPCCodeEpoch) { e.Identity.BeaconAddress = other.Hex() },
	} {
		e := epoch
		mutate(&e)
		if _, err := a.validateCodeUncached(context.Background(), e, common.Hash{}); err != ErrQualification {
			t.Fatalf("%s: want ErrQualification, got %v", name, err)
		}
	}
}
