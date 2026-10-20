package deployment

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// surfaceRuntime is the 41-byte runtime every v6.12.0 surface address holds; it
// forwards calls to the launchpad precompile at 0x1017.
var surfaceRuntime = common.FromHex("0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3")

const surfaceRuntimeHash = "0x6302ff80f4168c2af23afbf3117c613edd66d288d120eee825f9ecadd43a392f"

// attestedSurfaceManifest keeps EventEmitter as the old erc1967 proxy and USDL direct,
// turns every other role into a surface and adds the launchpad precompile.
func attestedSurfaceManifest(t *testing.T, usdlCode []byte) Manifest {
	t.Helper()
	m := Current()
	m.Status = "active"
	m.ActivationHeight = 1
	for name, r := range m.Roles {
		switch name {
		case "EventEmitter":
		case "USDL":
			r.RuntimeHash = crypto.Keccak256Hash(usdlCode).Hex()
		default:
			r = Role{Address: r.Address, Kind: "surface", RuntimeHash: surfaceRuntimeHash}
		}
		m.Roles[name] = r
	}
	m.Roles[LaunchpadPrecompileRole] = Role{Address: LaunchpadPrecompileAddress, Kind: "precompile"}
	if err := m.Validate(); err != nil {
		t.Fatalf("surface manifest invalid: %v", err)
	}
	return m
}

type rpcBackend struct {
	mu     sync.Mutex
	codes  map[common.Address][]byte
	store  map[common.Address]map[common.Hash]common.Hash
	calls  map[string][]byte
	header *types.Header
	asked  map[common.Address]bool
}

func (b *rpcBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	addr := func(i int) common.Address {
		var s string
		if len(req.Params) > i {
			_ = json.Unmarshal(req.Params[i], &s)
		}
		return common.HexToAddress(s)
	}
	var result any
	switch req.Method {
	case "eth_chainId":
		result = hexutil.EncodeBig(big.NewInt(ChainID))
	case "eth_getBlockByHash":
		result = b.header
	case "eth_getCode":
		a := addr(0)
		b.asked[a] = true
		result = hexutil.Bytes(b.codes[a])
	case "eth_getStorageAt":
		var key string
		_ = json.Unmarshal(req.Params[1], &key)
		v := b.store[addr(0)][common.HexToHash(key)]
		result = hexutil.Bytes(v.Bytes())
	case "eth_call":
		var call struct {
			Data  string `json:"data"`
			Input string `json:"input"`
		}
		_ = json.Unmarshal(req.Params[0], &call)
		data := call.Input
		if data == "" {
			data = call.Data
		}
		word, ok := b.calls[strings.ToLower(strings.TrimPrefix(data, "0x"))]
		if !ok {
			word = common.LeftPadBytes([]byte{1}, 32)
		}
		result = hexutil.Bytes(word)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(req.ID)+`,"error":{"code":-32601,"message":"method not found"}}`)
		return
	}
	out, _ := json.Marshal(result)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(req.ID)+`,"result":`+string(out)+`}`)
}

func attestSurface(t *testing.T, mutate func(*rpcBackend, Manifest)) (ActivationAttestation, Manifest, Policy, *rpcBackend) {
	t.Helper()
	usdlCode := common.FromHex("0x6080604052600080fd")
	m := attestedSurfaceManifest(t, usdlCode)
	f := fixtures(t)
	ee := m.Roles["EventEmitter"]
	eeAddr := common.HexToAddress(ee.Address)
	eeImpl := common.HexToAddress(ee.ImplementationAddress)
	b := &rpcBackend{
		codes: map[common.Address][]byte{
			eeAddr: runtime(f, "UUPSProxy", eeAddr),
			eeImpl: runtime(f, "EventEmitter", eeImpl),
			common.HexToAddress(m.Roles["USDL"].Address): usdlCode,
		},
		store: map[common.Address]map[common.Hash]common.Hash{eeAddr: {ImplementationSlot: addrWord(ee.ImplementationAddress)}},
		calls: map[string][]byte{
			common.Bytes2Hex(selector("owner()")):        addrWord(m.Governance.ExpectedBeaconOwner).Bytes(),
			common.Bytes2Hex(selector("pendingOwner()")): make([]byte, 32),
			common.Bytes2Hex(selector("creationFee()")):  common.LeftPadBytes(big.NewInt(1000000).Bytes(), 32),
			common.Bytes2Hex(selector("usdlAddress()")):  addrWord(m.Roles["USDL"].Address).Bytes(),
			common.Bytes2Hex(selector("decimals()")):     common.LeftPadBytes([]byte{6}, 32),
		},
		asked: map[common.Address]bool{},
	}
	for _, r := range m.Roles {
		if r.Kind == "surface" {
			b.codes[common.HexToAddress(r.Address)] = surfaceRuntime
		}
	}
	h := &types.Header{Number: big.NewInt(100), Time: uint64(time.Now().Unix()), Difficulty: big.NewInt(0), GasLimit: 30000000}
	b.header = h
	if mutate != nil {
		mutate(b, m)
	}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	c, err := ethclient.Dial(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	p := Policy{Now: time.Unix(int64(h.Time), 0).Add(time.Second), MaxAge: time.Minute, Timeout: 10 * time.Second}
	a, err := Attest(context.Background(), Reader(c), m, h.Hash(), p)
	if err != nil {
		t.Fatalf("Attest: %v", err)
	}
	return a, m, p, b
}

func TestAttestSurfaceAndPrecompileRoles(t *testing.T) {
	if got := crypto.Keccak256Hash(surfaceRuntime).Hex(); got != surfaceRuntimeHash {
		t.Fatalf("surface runtime hash = %s, want %s", got, surfaceRuntimeHash)
	}
	t.Run("identified", func(t *testing.T) {
		a, m, p, b := attestSurface(t, nil)
		if len(a.Issues) != 0 || !a.Eligible {
			t.Fatalf("issues = %v eligible = %v", a.Issues, a.Eligible)
		}
		for _, name := range []string{"Router", LaunchpadPrecompileRole, "EventEmitter"} {
			if !a.Identities[name].Verified {
				t.Fatalf("%s not verified: %+v", name, a.Identities[name])
			}
		}
		for _, name := range []string{"Router", LaunchpadPrecompileRole} {
			id := a.Identities[name]
			if id.ImplementationAddress != "" || id.ImplementationHash != "" || id.BeaconAddress != "" {
				t.Fatalf("%s carries implementation identity: %+v", name, id)
			}
		}
		if a.Identities["Router"].RuntimeHash != surfaceRuntimeHash {
			t.Fatalf("Router runtime hash = %s", a.Identities["Router"].RuntimeHash)
		}
		if a.Identities["EventEmitter"].ImplementationAddress != common.HexToAddress(m.Roles["EventEmitter"].ImplementationAddress).Hex() {
			t.Fatalf("EventEmitter implementation = %s", a.Identities["EventEmitter"].ImplementationAddress)
		}
		if b.asked[common.HexToAddress(LaunchpadPrecompileAddress)] {
			t.Fatal("Attest read code at the precompile address")
		}
		if err := a.ValidateFor(m, p); err != nil {
			t.Fatalf("ValidateFor: %v", err)
		}
		if err := a.ValidateFamilyFor(m, p, []string{"Router", LaunchpadPrecompileRole, "EventEmitter"}, true); err != nil {
			t.Fatalf("ValidateFamilyFor: %v", err)
		}
	})
	t.Run("surface holds other code", func(t *testing.T) {
		a, m, p, _ := attestSurface(t, func(b *rpcBackend, m Manifest) {
			b.codes[common.HexToAddress(m.Roles["Router"].Address)] = common.FromHex("0x6080604052600080fd")
		})
		if !hasIssue(a, "unknown_runtime_hash", "Router") || a.Identities["Router"].Verified || a.Eligible {
			t.Fatalf("other code at a surface was accepted: issues = %v", a.Issues)
		}
		if err := a.ValidateFamilyFor(m, p, []string{"Router"}, false); err == nil {
			t.Fatal("ValidateFamilyFor accepted an unidentified surface")
		}
	})
	t.Run("precompile role off the precompile address", func(t *testing.T) {
		a, m, _, _ := attestSurface(t, nil)
		r := m.Roles[LaunchpadPrecompileRole]
		r.Address = m.Roles["Router"].Address
		if runtimeIdentity(r, a.Identities[LaunchpadPrecompileRole]) {
			t.Fatal("precompile identity accepted at a non-precompile address")
		}
	})
}
