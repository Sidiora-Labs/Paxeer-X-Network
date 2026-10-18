// Package protocol holds the launchpad contract bindings (subpackage bindings),
// the decoded-event topic0 registry (events.go), and the typed on-chain address
// book (addresses.go) for the Sidiora launchpad on Paxeer Network (chain id 125).
//
// Source of truth for addresses: the 2026-06-20 Paxeer deployment manifest,
// cross-checked against /sidiora/docker-envs/*.env. These supersede the stale
// stack.contracts block in knowledge/kindlelaunch.frozen.kvx.
package protocol

import "github.com/ethereum/go-ethereum/common"

// ChainID is the Paxeer Network EVM chain id.
const ChainID int64 = 125

// Canonical runtime addresses for the ten contracts that services bind via
// chain.CreateContracts (the proxy address where the contract sits behind an
// ERC-1967 proxy). Exported as hex so shared/config and tooling can use them as
// defaults. Keys match the env var names wired in shared/src/chain/contracts.ts.
const (
	EventEmitterHex   = "0x0E10286EE51F99c666CDcAb52451e58AbdA4048F"
	PoolRegistryHex   = "0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b"
	SidioraFactoryHex = "0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465"
	RouterHex         = "0xCC7298801112682e10ee14b8a520309caD80336d"
	QuoterHex         = "0xB768e183b6EfDeDf8b2AA7af732039D1C3c452d0"
	ProtocolConfigHex = "0xEeDF5409cFD30bd14D0399318c7d2150265575e5"
	FeeAccumulatorHex = "0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76"
	SidioraNFTHex     = "0xDF73b354ed9dcB473cc9D01541c46f507591e190"
	FeesRouterHex     = "0x02Df12a44F2658080E76fbcF7D6B34Baa97843b6"
	PoolBeaconHex     = "0xf11f08afe33e020Cab22bCaffBbAfC471c75E9d4"
)

// Deployment is the typed on-chain address book for a single launchpad
// deployment. Runtime contract fields hold the proxy address where the contract
// is upgradeable; Impl maps each upgradeable contract's logical name to the
// implementation behind its proxy (useful for decoding Upgraded events and for
// explorer/verification flows).
type Deployment struct {
	ChainID int64

	// Core governance / accounting.
	ProtocolConfig common.Address
	Treasury       common.Address
	Timelock       common.Address
	Governance     common.Address

	// Launchpad core.
	EventEmitter   common.Address
	PoolRegistry   common.Address
	FeeAccumulator common.Address
	PoolBeacon     common.Address
	SidioraNFT     common.Address
	SidioraFactory common.Address

	// Trading.
	OpticalRegistry common.Address
	Router          common.Address
	Quoter          common.Address
	FeesRouter      common.Address

	// Optical (anti-abuse) modules.
	AntiSnipeOptical   common.Address
	MaxWalletOptical   common.Address
	TaxOptical         common.Address
	CooldownOptical    common.Address
	BuybackBurnOptical common.Address

	// Implementation addresses behind the ERC-1967 proxies, keyed by logical
	// contract name (e.g. "EventEmitter", "SidioraPool").
	Impl map[string]common.Address

	// Tokens and roles.
	USDL     common.Address
	Sidiora  common.Address
	Deployer common.Address
	Guardian common.Address
}

// PaxeerMainnet is the canonical Sidiora launchpad deployment on Paxeer Network
// (chain id 125), deployed 2026-06-20T06:04:56Z by Deployer.
var PaxeerMainnet = Deployment{
	ChainID: ChainID,

	ProtocolConfig: common.HexToAddress(ProtocolConfigHex),
	Treasury:       common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079"),
	Timelock:       common.HexToAddress("0x82e177ca309578dc5Ed7Fc583278D2C96b3c0F14"),
	Governance:     common.HexToAddress("0x87d682Fe2eeB6e76648f38BF3a955E36F26cCdB1"),

	EventEmitter:   common.HexToAddress(EventEmitterHex),
	PoolRegistry:   common.HexToAddress(PoolRegistryHex),
	FeeAccumulator: common.HexToAddress(FeeAccumulatorHex),
	PoolBeacon:     common.HexToAddress(PoolBeaconHex),
	SidioraNFT:     common.HexToAddress(SidioraNFTHex),
	SidioraFactory: common.HexToAddress(SidioraFactoryHex),

	OpticalRegistry: common.HexToAddress("0x4CdA6e48632d51Ee4Fa735D81BF09F7543f644a1"),
	Router:          common.HexToAddress(RouterHex),
	Quoter:          common.HexToAddress(QuoterHex),
	FeesRouter:      common.HexToAddress(FeesRouterHex),

	AntiSnipeOptical:   common.HexToAddress("0x5ed0084Aa348eC45673af22e01CaF2f3500b77b5"),
	MaxWalletOptical:   common.HexToAddress("0x0086B61fAd8fc50b2f81F92337518Ca8b4A7cc01"),
	TaxOptical:         common.HexToAddress("0x285411005079AaBB12bb2516bF6578fbfB11Be90"),
	CooldownOptical:    common.HexToAddress("0xe7d450534Bc401494075e753Bb142685CF868238"),
	BuybackBurnOptical: common.HexToAddress("0x14ebb4F1e32070085a138296970aB90a4B5E3940"),

	Impl: map[string]common.Address{
		"EventEmitter":     common.HexToAddress("0x72CFc8f07b5Db8EE7bAcE7e8e8E8659D516cdA3E"),
		"ProtocolConfig":   common.HexToAddress("0xc9278B9673a222Cffae29b7Da8e4cff83272c51C"),
		"Treasury":         common.HexToAddress("0xcdBB7923e9BE9255717704cf85Ee1B7fA201b52b"),
		"GovernanceModule": common.HexToAddress("0xbBC3C83579F0016caAE99bd6d5E04918213750eD"),
		"PoolRegistry":     common.HexToAddress("0x6Fdecd4361927c7DB86BCc01E3121649A38BF8A3"),
		"FeeAccumulator":   common.HexToAddress("0xf0643BbFA8554A29F5261f5e2f158f966EaA11e8"),
		"SidioraPool":      common.HexToAddress("0x04E36690793056363EfcB0A62D753a63D92de0C1"),
		"SidioraNFT":       common.HexToAddress("0x287891497B60e72E5EED1BDfE71195B12b615B3b"),
		"SidioraFactory":   common.HexToAddress("0xFEed19980287B08930F1f1E00f6d89a3B9ef517d"),
		"OpticalRegistry":  common.HexToAddress("0x63683eE5Fde3C3B66102bC4Faf3782fbF8b7Faf6"),
		"Router":           common.HexToAddress("0x4A867a10770e8fD601C72301aAc1C511099E30EF"),
		"Quoter":           common.HexToAddress("0x42D0B5737203ba77125d46106690e188B321393F"),
		"FeesRouter":       common.HexToAddress("0xD8a058199DDf1B7443420735252187add41b6135"),
	},

	USDL:     common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479"),
	Sidiora:  common.HexToAddress("0x21f7b20a555199fa73A238B1a91FD0f549068fEe"),
	Deployer: common.HexToAddress("0x1255d84066f579E7B7A3df4296e960d59fc05b32"),
	Guardian: common.HexToAddress("0x1255d84066f579E7B7A3df4296e960d59fc05b32"),
}

// BoundContracts returns the ten runtime contracts that chain.CreateContracts
// binds, keyed by canonical name (matching the bindings type names).
func (d *Deployment) BoundContracts() map[string]common.Address {
	return map[string]common.Address{
		"EventEmitter":   d.EventEmitter,
		"PoolRegistry":   d.PoolRegistry,
		"SidioraFactory": d.SidioraFactory,
		"Router":         d.Router,
		"Quoter":         d.Quoter,
		"ProtocolConfig": d.ProtocolConfig,
		"FeeAccumulator": d.FeeAccumulator,
		"SidioraNFT":     d.SidioraNFT,
		"FeesRouter":     d.FeesRouter,
		"PoolBeacon":     d.PoolBeacon,
	}
}

// EnvDefaults returns the ten bound-contract addresses keyed by the environment
// variable names used in shared/config (and shared/src/chain/contracts.ts),
// EIP-55 checksummed. Useful as defaults and for config wiring.
func (d *Deployment) EnvDefaults() map[string]string {
	return map[string]string{
		"EVENT_EMITTER_ADDRESS":   d.EventEmitter.Hex(),
		"POOL_REGISTRY_ADDRESS":   d.PoolRegistry.Hex(),
		"FACTORY_ADDRESS":         d.SidioraFactory.Hex(),
		"ROUTER_ADDRESS":          d.Router.Hex(),
		"QUOTER_ADDRESS":          d.Quoter.Hex(),
		"PROTOCOL_CONFIG_ADDRESS": d.ProtocolConfig.Hex(),
		"FEE_ACCUMULATOR_ADDRESS": d.FeeAccumulator.Hex(),
		"SIDIORA_NFT_ADDRESS":     d.SidioraNFT.Hex(),
		"FEES_ROUTER_ADDRESS":     d.FeesRouter.Hex(),
		"POOL_BEACON_ADDRESS":     d.PoolBeacon.Hex(),
	}
}
