package config_test

import (
	"testing"

	engine "github.com/Sidiora-Labs/Paxeer-X-Network/engine/deps/xevm/config"
	canonical "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
)

func TestExecutionAndRPCChainBindingsAgree(t *testing.T) {
	for cosmos, chain := range canonical.ChainIDMapping {
		if got := engine.GetEVMChainID(cosmos).Int64(); got != chain {
			t.Fatalf("%s: execution=%d RPC=%d", cosmos, got, chain)
		}
		if !engine.IsLiveEVMChainID(chain) {
			t.Fatalf("mapped chain %d is not protected as live", chain)
		}
		if engine.EVMChainIDMapping[chain] != cosmos {
			t.Fatalf("reverse binding differs for %d", chain)
		}
	}
	if engine.GetEVMChainID("hyperpax_125-1").Int64() != 125 {
		t.Fatal("Paxeer beta does not bind chain 125")
	}
	if engine.GetEVMChainID("unknown-test-chain").Int64() != canonical.DefaultChainID {
		t.Fatal("unknown-chain default changed")
	}
}
