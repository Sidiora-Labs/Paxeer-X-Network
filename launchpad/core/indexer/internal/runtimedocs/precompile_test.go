package runtimedocs

import (
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

const surfaceRuntimeHash = "0x6302ff80f4168c2af23afbf3117c613edd66d288d120eee825f9ecadd43a392f"

func TestReceiptOptionsBindPrecompile(t *testing.T) {
	in := realInputs(t)
	in.PoolRuntimeHash = common.HexToHash(surfaceRuntimeHash)
	in.Anchor = receipt.BlockAnchor{Height: in.Manifest.ActivationHeight, Hash: realAnchor.Hash}
	o, catalog, err := ReceiptOptions(in)
	if err != nil {
		t.Fatal(err)
	}
	precompile := common.HexToAddress(deployment.LaunchpadPrecompileAddress)
	found, surfaces := false, 0
	deployments := make([]decode.Deployment, 0, len(o.Epochs))
	for _, e := range o.Epochs {
		deployments = append(deployments, e.Deployment)
		if e.Identity.Kind == "surface" {
			surfaces++
			if e.Identity.RuntimeHash != surfaceRuntimeHash {
				t.Fatalf("surface %s runtime hash %s", e.Deployment.Role, e.Identity.RuntimeHash)
			}
		}
		if e.Deployment.Address != precompile {
			continue
		}
		if found {
			t.Fatal("duplicate epoch at 0x1017")
		}
		found = true
		if e.Deployment.Role != deployment.LaunchpadPrecompileRole || e.Identity.Kind != "precompile" || e.Deployment.From.Block != in.Manifest.ActivationHeight || e.Deployment.ABIRevision != catalog.Revisions["EventEmitter"] || e.Deployment.ABIRevision == "" {
			t.Fatalf("precompile epoch %+v", e)
		}
	}
	if !found {
		t.Fatal("no epoch at 0x1017")
	}
	if surfaces == 0 {
		t.Fatal("no generation 2 surface epochs")
	}
	pool := false
	for _, d := range o.Discovery {
		if d.Role == "SidioraPool" {
			pool = d.RuntimeHash == surfaceRuntimeHash
		}
	}
	if !pool {
		t.Fatalf("SidioraPool discovery %+v", o.Discovery)
	}
	if _, err = decode.NewRegistry(o.CatalogJSON, deployments); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(o.CatalogJSON), `"name":"LaunchpadPrecompile"`) {
		t.Fatal("catalog carries a LaunchpadPrecompile contract")
	}
}
