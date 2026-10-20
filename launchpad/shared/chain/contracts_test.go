package chain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
)

// canonicalEnv builds a BaseEnv whose ten contract addresses come from the real
// protocol address book — ties this test to the canonical deployment, no fakes.
func canonicalEnv() config.BaseEnv {
	d := protocol.PaxeerMainnet
	return config.BaseEnv{
		EventEmitterAddress: d.EventEmitter.Hex(),
		PoolRegistryAddress: d.PoolRegistry.Hex(),
		FactoryAddress:      d.SidioraFactory.Hex(),
		RouterAddress:       d.Router.Hex(),
		QuoterAddress:       d.Quoter.Hex(),
		ProtocolConfigAddr:  d.ProtocolConfig.Hex(),
		FeeAccumulatorAddr:  d.FeeAccumulator.Hex(),
		SidioraNFTAddress:   d.SidioraNFT.Hex(),
		FeesRouterAddress:   d.FeesRouter.Hex(),
		PoolBeaconAddress:   d.PoolBeacon.Hex(),
	}
}

func TestParseContractAddresses(t *testing.T) {
	t.Parallel()
	env := canonicalEnv()
	a, err := parseContractAddresses(&env)
	if err != nil {
		t.Fatalf("parseContractAddresses: %v", err)
	}
	d := protocol.PaxeerMainnet
	for _, c := range []struct {
		name      string
		got, want common.Address
	}{
		{"EventEmitter", a.EventEmitter, d.EventEmitter},
		{"PoolRegistry", a.PoolRegistry, d.PoolRegistry},
		{"Factory", a.Factory, d.SidioraFactory},
		{"Router", a.Router, d.Router},
		{"Quoter", a.Quoter, d.Quoter},
		{"ProtocolConfig", a.ProtocolConfig, d.ProtocolConfig},
		{"FeeAccumulator", a.FeeAccumulator, d.FeeAccumulator},
		{"SidioraNFT", a.SidioraNFT, d.SidioraNFT},
		{"FeesRouter", a.FeesRouter, d.FeesRouter},
		{"PoolBeacon", a.PoolBeacon, d.PoolBeacon},
	} {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", c.name, c.got.Hex(), c.want.Hex())
		}
	}
}

func TestParseContractAddressesErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		patch func(*config.BaseEnv)
	}{
		{"invalid hex", func(e *config.BaseEnv) { e.RouterAddress = "0xnothex" }},
		{"too short", func(e *config.BaseEnv) { e.QuoterAddress = "0x1234" }},
		{"zero address", func(e *config.BaseEnv) {
			e.EventEmitterAddress = "0x0000000000000000000000000000000000000000"
		}},
		{"empty", func(e *config.BaseEnv) { e.PoolBeaconAddress = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := canonicalEnv()
			tc.patch(&env)
			if _, err := parseContractAddresses(&env); err == nil {
				t.Errorf("%s: expected error, got nil", tc.name)
			}
		})
	}
}

func TestCreateContractsBindsAll(t *testing.T) {
	t.Parallel()
	srv := rpcServer(t, map[string]string{"eth_chainId": "0x7d"}, false)
	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	env := canonicalEnv()
	cs, err := CreateContracts(c, &env)
	if err != nil {
		t.Fatalf("CreateContracts: %v", err)
	}
	if cs.EventEmitter == nil || cs.PoolRegistry == nil || cs.Factory == nil ||
		cs.Router == nil || cs.Quoter == nil || cs.ProtocolConfig == nil ||
		cs.FeeAccumulator == nil || cs.SidioraNFT == nil || cs.FeesRouter == nil ||
		cs.PoolBeacon == nil {
		t.Fatal("one or more contract handles are nil")
	}
	if cs.Addresses.Router != protocol.PaxeerMainnet.Router {
		t.Errorf("Addresses.Router = %s, want %s", cs.Addresses.Router.Hex(), protocol.PaxeerMainnet.Router.Hex())
	}
}

func TestCreateContractsNilArgs(t *testing.T) {
	t.Parallel()
	env := canonicalEnv()
	if _, err := CreateContracts(nil, &env); err == nil {
		t.Error("expected error for nil client")
	}
	srv := rpcServer(t, map[string]string{"eth_chainId": "0x7d"}, false)
	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := CreateContracts(c, nil); err == nil {
		t.Error("expected error for nil env")
	}
}

func TestCreateContractsInvalidEnv(t *testing.T) {
	t.Parallel()
	srv := rpcServer(t, map[string]string{"eth_chainId": "0x7d"}, false)
	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	env := canonicalEnv()
	env.EventEmitterAddress = "nope"
	if _, err := CreateContracts(c, &env); err == nil {
		t.Error("expected error for invalid env address")
	}
}

// TestCreateContractsRouting proves a bound handle issues a REAL eth_call to the
// bound address and decodes the result — end-to-end binding plumbing, not a fake.
// It calls PoolBeacon.implementation() against a JSON-RPC server that records the
// call's `to` and returns an ABI-encoded address.
func TestCreateContractsRouting(t *testing.T) {
	t.Parallel()
	want := protocol.PaxeerMainnet.Impl["EventEmitter"] // any non-zero return value
	var gotTo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "eth_call" {
			var call struct {
				To string `json:"to"`
			}
			if len(req.Params) > 0 {
				_ = json.Unmarshal(req.Params[0], &call)
			}
			gotTo = call.To
			ret := common.Bytes2Hex(common.LeftPadBytes(want.Bytes(), 32))
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x`+ret+`"}`)
			return
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x"}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()
	env := canonicalEnv()
	cs, err := CreateContracts(c, &env)
	if err != nil {
		t.Fatalf("CreateContracts: %v", err)
	}

	impl, err := cs.PoolBeacon.Implementation(&bind.CallOpts{Context: context.Background()})
	if err != nil {
		t.Fatalf("Implementation call: %v", err)
	}
	if impl != want {
		t.Errorf("decoded implementation = %s, want %s", impl.Hex(), want.Hex())
	}
	if !strings.EqualFold(gotTo, protocol.PaxeerMainnet.PoolBeacon.Hex()) {
		t.Errorf("call routed to %s, want PoolBeacon %s", gotTo, protocol.PaxeerMainnet.PoolBeacon.Hex())
	}
}

// TestRolesBindToPrecompile sets every role to the launchpad precompile except
// SidioraNFT and PoolBeacon, which keep their generation 2 addresses, and proves
// CreateContracts binds each role at its address and the EventEmitter ABI at
// 0x1017: a Swap log filter on the precompile handle queries 0x1017 and a
// PoolBeacon read is sent to the generation 2 PoolBeacon.
func TestRolesBindToPrecompile(t *testing.T) {
	t.Parallel()
	precompile := common.HexToAddress("0x0000000000000000000000000000000000001017")
	if LaunchpadPrecompileAddress != precompile {
		t.Fatalf("LaunchpadPrecompileAddress = %s, want %s", LaunchpadPrecompileAddress.Hex(), precompile.Hex())
	}
	roles := deployment.Current().Roles
	nft := common.HexToAddress(roles["SidioraNFT"].Address)
	beacon := common.HexToAddress(roles["PoolBeacon"].Address)
	impl := common.HexToAddress(roles["PoolBeacon"].ImplementationAddress)

	var mu sync.Mutex
	var callTo string
	var logAddrs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch req.Method {
		case "eth_call":
			var call struct {
				To string `json:"to"`
			}
			if len(req.Params) > 0 {
				_ = json.Unmarshal(req.Params[0], &call)
			}
			callTo = call.To
			ret := common.Bytes2Hex(common.LeftPadBytes(impl.Bytes(), 32))
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x`+ret+`"}`)
		case "eth_getLogs":
			var q struct {
				Address []string `json:"address"`
			}
			if len(req.Params) > 0 {
				_ = json.Unmarshal(req.Params[0], &q)
			}
			logAddrs = q.Address
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[]}`)
		default:
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x7d"}`)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	p := precompile.Hex()
	env := config.BaseEnv{
		EventEmitterAddress: p,
		PoolRegistryAddress: p,
		FactoryAddress:      p,
		RouterAddress:       p,
		QuoterAddress:       p,
		ProtocolConfigAddr:  p,
		FeeAccumulatorAddr:  p,
		SidioraNFTAddress:   nft.Hex(),
		FeesRouterAddress:   p,
		PoolBeaconAddress:   beacon.Hex(),
	}
	cs, err := CreateContracts(c, &env)
	if err != nil {
		t.Fatalf("CreateContracts: %v", err)
	}
	if cs.EventEmitter == nil || cs.PoolRegistry == nil || cs.Factory == nil ||
		cs.Router == nil || cs.Quoter == nil || cs.ProtocolConfig == nil ||
		cs.FeeAccumulator == nil || cs.SidioraNFT == nil || cs.FeesRouter == nil ||
		cs.PoolBeacon == nil || cs.LaunchpadPrecompile == nil {
		t.Fatal("one or more contract handles are nil")
	}
	want := ContractAddresses{
		EventEmitter:        precompile,
		PoolRegistry:        precompile,
		Factory:             precompile,
		Router:              precompile,
		Quoter:              precompile,
		ProtocolConfig:      precompile,
		FeeAccumulator:      precompile,
		SidioraNFT:          nft,
		FeesRouter:          precompile,
		PoolBeacon:          beacon,
		LaunchpadPrecompile: precompile,
	}
	if cs.Addresses != want {
		t.Errorf("Addresses = %+v, want %+v", cs.Addresses, want)
	}

	it, err := cs.LaunchpadPrecompile.FilterSwap(&bind.FilterOpts{Context: context.Background()}, nil, nil)
	if err != nil {
		t.Fatalf("FilterSwap: %v", err)
	}
	if it.Next() {
		t.Error("FilterSwap returned a log from an empty result")
	}
	if err := it.Error(); err != nil {
		t.Errorf("FilterSwap iterator: %v", err)
	}
	_ = it.Close()

	got, err := cs.PoolBeacon.Implementation(&bind.CallOpts{Context: context.Background()})
	if err != nil {
		t.Fatalf("Implementation call: %v", err)
	}
	if got != impl {
		t.Errorf("decoded implementation = %s, want %s", got.Hex(), impl.Hex())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logAddrs) != 1 || !strings.EqualFold(logAddrs[0], p) {
		t.Errorf("Swap logs queried at %v, want [%s]", logAddrs, p)
	}
	if !strings.EqualFold(callTo, beacon.Hex()) {
		t.Errorf("call routed to %s, want PoolBeacon %s", callTo, beacon.Hex())
	}
}
