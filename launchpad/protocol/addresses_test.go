package protocol

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// literals is the authoritative address set from the 2026-06-20 Paxeer
// deployment manifest. Tests assert (a) each literal is EIP-55 checksummed and
// (b) PaxeerMainnet transcribed it correctly — two independent transcriptions
// that must agree.
var literals = map[string]string{
	"ProtocolConfig":  ProtocolConfigHex,
	"Treasury":        "0x15405D535ce533BfFb98c83e42f4DD242AA5e079",
	"Timelock":        "0x82e177ca309578dc5Ed7Fc583278D2C96b3c0F14",
	"Governance":      "0x87d682Fe2eeB6e76648f38BF3a955E36F26cCdB1",
	"EventEmitter":    EventEmitterHex,
	"PoolRegistry":    PoolRegistryHex,
	"FeeAccumulator":  FeeAccumulatorHex,
	"PoolBeacon":      PoolBeaconHex,
	"SidioraNFT":      SidioraNFTHex,
	"SidioraFactory":  SidioraFactoryHex,
	"OpticalRegistry": "0x4CdA6e48632d51Ee4Fa735D81BF09F7543f644a1",
	"Router":          RouterHex,
	"Quoter":          QuoterHex,
	"FeesRouter":      FeesRouterHex,
	"AntiSnipe":       "0x5ed0084Aa348eC45673af22e01CaF2f3500b77b5",
	"MaxWallet":       "0x0086B61fAd8fc50b2f81F92337518Ca8b4A7cc01",
	"Tax":             "0x285411005079AaBB12bb2516bF6578fbfB11Be90",
	"Cooldown":        "0xe7d450534Bc401494075e753Bb142685CF868238",
	"BuybackBurn":     "0x14ebb4F1e32070085a138296970aB90a4B5E3940",
	"USDL":            "0x85FcD13735F4309833A503EE804ea32395851479",
	"Sidiora":         "0x21f7b20a555199fa73A238B1a91FD0f549068fEe",
	"Deployer":        "0x1255d84066f579E7B7A3df4296e960d59fc05b32",
}

func TestLiteralsAreEIP55Checksummed(t *testing.T) {
	t.Parallel()
	for name, hex := range literals {
		// common.Address.Hex() emits the canonical EIP-55 mixed-case form; if a
		// source literal is mistyped (wrong case/char), the round-trip diverges.
		if got := common.HexToAddress(hex).Hex(); got != hex {
			t.Errorf("%s: %q is not EIP-55 checksummed (canonical %q)", name, hex, got)
		}
	}
}

func TestChainID(t *testing.T) {
	t.Parallel()
	if ChainID != 125 {
		t.Fatalf("ChainID = %d, want 125", ChainID)
	}
	if PaxeerMainnet.ChainID != ChainID {
		t.Fatalf("PaxeerMainnet.ChainID = %d, want %d", PaxeerMainnet.ChainID, ChainID)
	}
}

func TestBoundContractsComplete(t *testing.T) {
	t.Parallel()
	bc := PaxeerMainnet.BoundContracts()
	want := []string{
		"EventEmitter", "PoolRegistry", "SidioraFactory", "Router", "Quoter",
		"ProtocolConfig", "FeeAccumulator", "SidioraNFT", "FeesRouter", "PoolBeacon",
	}
	if len(bc) != len(want) {
		t.Fatalf("BoundContracts has %d entries, want %d", len(bc), len(want))
	}
	for _, name := range want {
		addr, ok := bc[name]
		if !ok {
			t.Errorf("BoundContracts missing %q", name)
			continue
		}
		if addr == (common.Address{}) {
			t.Errorf("%s bound to the zero address", name)
		}
		if exp := common.HexToAddress(literals[name]); addr != exp {
			t.Errorf("%s = %s, want %s", name, addr.Hex(), exp.Hex())
		}
	}
}

func TestEnvDefaults(t *testing.T) {
	t.Parallel()
	env := PaxeerMainnet.EnvDefaults()
	want := map[string]string{
		"EVENT_EMITTER_ADDRESS":   "EventEmitter",
		"POOL_REGISTRY_ADDRESS":   "PoolRegistry",
		"FACTORY_ADDRESS":         "SidioraFactory",
		"ROUTER_ADDRESS":          "Router",
		"QUOTER_ADDRESS":          "Quoter",
		"PROTOCOL_CONFIG_ADDRESS": "ProtocolConfig",
		"FEE_ACCUMULATOR_ADDRESS": "FeeAccumulator",
		"SIDIORA_NFT_ADDRESS":     "SidioraNFT",
		"FEES_ROUTER_ADDRESS":     "FeesRouter",
		"POOL_BEACON_ADDRESS":     "PoolBeacon",
	}
	if len(env) != len(want) {
		t.Fatalf("EnvDefaults has %d keys, want %d", len(env), len(want))
	}
	for k, contract := range want {
		v, ok := env[k]
		if !ok {
			t.Errorf("EnvDefaults missing %q", k)
			continue
		}
		// Values must be EIP-55 checksummed and match the canonical address.
		if v != common.HexToAddress(v).Hex() {
			t.Errorf("%s value %q is not checksummed", k, v)
		}
		if v != literals[contract] {
			t.Errorf("%s = %s, want %s", k, v, literals[contract])
		}
	}
}

func TestContractAddressesUniqueAndNonZero(t *testing.T) {
	t.Parallel()
	// Distinct contracts must have distinct addresses (roles like Deployer ==
	// Guardian are intentionally shared and excluded here).
	contracts := map[string]common.Address{
		"ProtocolConfig":  PaxeerMainnet.ProtocolConfig,
		"Treasury":        PaxeerMainnet.Treasury,
		"Timelock":        PaxeerMainnet.Timelock,
		"Governance":      PaxeerMainnet.Governance,
		"EventEmitter":    PaxeerMainnet.EventEmitter,
		"PoolRegistry":    PaxeerMainnet.PoolRegistry,
		"FeeAccumulator":  PaxeerMainnet.FeeAccumulator,
		"PoolBeacon":      PaxeerMainnet.PoolBeacon,
		"SidioraNFT":      PaxeerMainnet.SidioraNFT,
		"SidioraFactory":  PaxeerMainnet.SidioraFactory,
		"OpticalRegistry": PaxeerMainnet.OpticalRegistry,
		"Router":          PaxeerMainnet.Router,
		"Quoter":          PaxeerMainnet.Quoter,
		"FeesRouter":      PaxeerMainnet.FeesRouter,
	}
	seen := make(map[common.Address]string, len(contracts))
	for name, addr := range contracts {
		if addr == (common.Address{}) {
			t.Errorf("%s is the zero address", name)
		}
		if prev, dup := seen[addr]; dup {
			t.Errorf("address collision: %s and %s both %s", prev, name, addr.Hex())
		}
		seen[addr] = name
	}
}

func TestImplementationsPresent(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"EventEmitter", "ProtocolConfig", "Treasury", "GovernanceModule",
		"PoolRegistry", "FeeAccumulator", "SidioraPool", "SidioraNFT",
		"SidioraFactory", "OpticalRegistry", "Router", "Quoter", "FeesRouter",
	} {
		addr, ok := PaxeerMainnet.Impl[name]
		if !ok {
			t.Errorf("Impl missing %q", name)
			continue
		}
		if addr == (common.Address{}) {
			t.Errorf("Impl[%q] is the zero address", name)
		}
	}
}
