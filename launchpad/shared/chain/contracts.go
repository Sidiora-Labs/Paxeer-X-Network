package chain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/bindings"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config"
)

// ContractAddresses holds the ten launchpad contract addresses parsed from the
// environment, mirroring the env vars wired in shared/src/chain/contracts.ts.
type ContractAddresses struct {
	EventEmitter   common.Address
	PoolRegistry   common.Address
	Factory        common.Address
	Router         common.Address
	Quoter         common.Address
	ProtocolConfig common.Address
	FeeAccumulator common.Address
	SidioraNFT     common.Address
	FeesRouter     common.Address
	PoolBeacon     common.Address
}

// Contracts bundles bound read/event-filter handles for the ten launchpad
// contracts, the Go parity of shared/src/chain/contracts.ts createContracts.
// The handles share the chain client's failover-unaware primary backend, which
// is the correct choice for log filtering and ad-hoc reads in the indexer and
// processors; callers needing failover use the Client methods directly.
type Contracts struct {
	EventEmitter   *bindings.EventEmitter
	PoolRegistry   *bindings.PoolRegistry
	Factory        *bindings.SidioraFactory
	Router         *bindings.Router
	Quoter         *bindings.Quoter
	ProtocolConfig *bindings.ProtocolConfig
	FeeAccumulator *bindings.FeeAccumulator
	SidioraNFT     *bindings.SidioraNFT
	FeesRouter     *bindings.FeesRouter
	PoolBeacon     *bindings.PoolBeacon

	// Addresses are the parsed addresses each handle is bound to.
	Addresses ContractAddresses
}

// parseAddress validates a single 0x-prefixed 20-byte address and rejects the
// zero address (a misconfigured zero would otherwise bind silently and only
// fail at call time — fail fast at startup instead).
func parseAddress(name, raw string) (common.Address, error) {
	s := strings.TrimSpace(raw)
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("chain: %s %q is not a valid 0x-prefixed 20-byte address", name, raw)
	}
	addr := common.HexToAddress(s)
	if addr == (common.Address{}) {
		return common.Address{}, fmt.Errorf("chain: %s must not be the zero address", name)
	}
	return addr, nil
}

// parseContractAddresses parses all ten env-provided addresses, collecting every
// validation error so a misconfiguration reports all bad fields at once.
func parseContractAddresses(env *config.BaseEnv) (ContractAddresses, error) {
	var a ContractAddresses
	var errs []error
	set := func(name, raw string, dst *common.Address) {
		addr, err := parseAddress(name, raw)
		if err != nil {
			errs = append(errs, err)
			return
		}
		*dst = addr
	}
	set("EVENT_EMITTER_ADDRESS", env.EventEmitterAddress, &a.EventEmitter)
	set("POOL_REGISTRY_ADDRESS", env.PoolRegistryAddress, &a.PoolRegistry)
	set("FACTORY_ADDRESS", env.FactoryAddress, &a.Factory)
	set("ROUTER_ADDRESS", env.RouterAddress, &a.Router)
	set("QUOTER_ADDRESS", env.QuoterAddress, &a.Quoter)
	set("PROTOCOL_CONFIG_ADDRESS", env.ProtocolConfigAddr, &a.ProtocolConfig)
	set("FEE_ACCUMULATOR_ADDRESS", env.FeeAccumulatorAddr, &a.FeeAccumulator)
	set("SIDIORA_NFT_ADDRESS", env.SidioraNFTAddress, &a.SidioraNFT)
	set("FEES_ROUTER_ADDRESS", env.FeesRouterAddress, &a.FeesRouter)
	set("POOL_BEACON_ADDRESS", env.PoolBeaconAddress, &a.PoolBeacon)
	return a, errors.Join(errs...)
}

// CreateContracts binds the ten launchpad contracts to the addresses in env,
// using the client's primary backend. Parity with createContracts (contracts.ts).
func CreateContracts(c *Client, env *config.BaseEnv) (*Contracts, error) {
	if c == nil || c.Eth() == nil {
		return nil, fmt.Errorf("chain: CreateContracts needs a dialed client")
	}
	if env == nil {
		return nil, fmt.Errorf("chain: CreateContracts needs a non-nil env")
	}
	addrs, err := parseContractAddresses(env)
	if err != nil {
		return nil, err
	}
	backend := c.Eth()
	out := &Contracts{Addresses: addrs}

	var berr error
	bindContract := func(name string, fn func() error) {
		if berr != nil {
			return
		}
		if e := fn(); e != nil {
			berr = fmt.Errorf("chain: bind %s: %w", name, e)
		}
	}
	bindContract("EventEmitter", func() (e error) { out.EventEmitter, e = bindings.NewEventEmitter(addrs.EventEmitter, backend); return })
	bindContract("PoolRegistry", func() (e error) { out.PoolRegistry, e = bindings.NewPoolRegistry(addrs.PoolRegistry, backend); return })
	bindContract("SidioraFactory", func() (e error) { out.Factory, e = bindings.NewSidioraFactory(addrs.Factory, backend); return })
	bindContract("Router", func() (e error) { out.Router, e = bindings.NewRouter(addrs.Router, backend); return })
	bindContract("Quoter", func() (e error) { out.Quoter, e = bindings.NewQuoter(addrs.Quoter, backend); return })
	bindContract("ProtocolConfig", func() (e error) {
		out.ProtocolConfig, e = bindings.NewProtocolConfig(addrs.ProtocolConfig, backend)
		return
	})
	bindContract("FeeAccumulator", func() (e error) {
		out.FeeAccumulator, e = bindings.NewFeeAccumulator(addrs.FeeAccumulator, backend)
		return
	})
	bindContract("SidioraNFT", func() (e error) { out.SidioraNFT, e = bindings.NewSidioraNFT(addrs.SidioraNFT, backend); return })
	bindContract("FeesRouter", func() (e error) { out.FeesRouter, e = bindings.NewFeesRouter(addrs.FeesRouter, backend); return })
	bindContract("PoolBeacon", func() (e error) { out.PoolBeacon, e = bindings.NewPoolBeacon(addrs.PoolBeacon, backend); return })
	if berr != nil {
		return nil, berr
	}
	return out, nil
}
