package deployment

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type artifactFixture struct {
	Runtime    string `json:"runtime"`
	Immutables map[string][]struct {
		Start  int
		Length int
	} `json:"immutableReferences"`
}
type evmFixture struct {
	Contracts map[string]artifactFixture `json:"contracts"`
}

func fixtures(t *testing.T) evmFixture {
	t.Helper()
	b, e := os.ReadFile("testdata/evm-artifacts.json")
	if e != nil {
		t.Fatal(e)
	}
	var f evmFixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func runtime(f evmFixture, name string, self common.Address) []byte {
	a := f.Contracts[name]
	b := common.FromHex(a.Runtime)
	for _, refs := range a.Immutables {
		for _, r := range refs {
			copy(b[r.Start:r.Start+r.Length], common.LeftPadBytes(self.Bytes(), r.Length))
		}
	}
	return b
}
func slot(n int64) common.Hash      { return common.BigToHash(big.NewInt(n)) }
func addrWord(a string) common.Hash { return common.BytesToHash(common.HexToAddress(a).Bytes()) }

// newEVM uses the current Solidity artifact bytecodes with explicit isolated
// genesis storage. These are actual pinned EVM reads, not fabricated RPC replies.
func newEVM(t *testing.T, mutate func(*Manifest, types.GenesisAlloc), chainID int64) (Manifest, Reader, common.Hash, Policy) {
	t.Helper()
	f := fixtures(t)
	m := Current()
	m.Status = "eligible"
	m.ActivationHeight = 1
	alloc := types.GenesisAlloc{}
	for name, r := range m.Roles {
		address := common.HexToAddress(r.Address)
		var code []byte
		storage := map[common.Hash]common.Hash{}
		switch r.Kind {
		case "erc1967":
			code = runtime(f, "UUPSProxy", address)
			storage[ImplementationSlot] = addrWord(r.ImplementationAddress)
			ia := common.HexToAddress(r.ImplementationAddress)
			ic := runtime(f, name, ia)
			alloc[ia] = types.Account{Code: ic, Balance: big.NewInt(0)}
			if crypto.Keccak256Hash(ic).Hex() != r.ImplementationHash {
				t.Fatalf("frozen %s implementation artifact mismatch", name)
			}
		case "beacon":
			code = runtime(f, "PoolBeacon", address)
			storage[slot(0)] = addrWord(r.ImplementationAddress)
			storage[slot(1)] = addrWord(m.Governance.ExpectedBeaconOwner)
			ia := common.HexToAddress(r.ImplementationAddress)
			ic := runtime(f, "SidioraPool", ia)
			alloc[ia] = types.Account{Code: ic, Balance: big.NewInt(0)}
			if crypto.Keccak256Hash(ic).Hex() != r.ImplementationHash {
				t.Fatal("pool artifact mismatch")
			}
		case "direct":
			code = runtime(f, "MockERC20", address)
			storage[slot(2)] = slot(6)
			r.RuntimeHash = crypto.Keccak256Hash(code).Hex()
			m.Roles[name] = r
		}
		if name != "USDL" && crypto.Keccak256Hash(code).Hex() != r.RuntimeHash {
			t.Fatalf("frozen %s runtime mismatch", name)
		}
		if name == "ProtocolConfig" {
			storage[slot(1)] = addrWord(m.Roles["USDL"].Address)
			storage[slot(11)] = slot(1000000)
		}
		alloc[address] = types.Account{Code: code, Storage: storage, Balance: big.NewInt(0)}
	}
	// Include a real BeaconProxy pool and verify beacon resolution at the same hash.
	poolAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	code := runtime(f, "BeaconProxy", poolAddr)
	beacon := m.Roles["PoolBeacon"]
	m.Roles["FixturePool"] = Role{Address: poolAddr.Hex(), Kind: "beacon_proxy", RuntimeHash: crypto.Keccak256Hash(code).Hex(), ImplementationAddress: beacon.ImplementationAddress, ImplementationHash: beacon.ImplementationHash, BeaconAddress: beacon.Address}
	alloc[poolAddr] = types.Account{Code: code, Storage: map[common.Hash]common.Hash{BeaconSlot: addrWord(beacon.Address)}, Balance: big.NewInt(0)}
	if mutate != nil {
		mutate(&m, alloc)
	}
	// A unique mode0700 directory and IPC socket prevent a failed TCP bind
	// from connecting to or mutating another lane's node. TCP is never dialed.
	dir, err := os.MkdirTemp("", "kd-c8d59a62-L03-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ipc := filepath.Join(dir, "rpc.ipc")
	// The fixture has no generated accounts, no fork URL and no production keys.
	cmd := exec.Command("anvil", "--host", "127.0.0.1", "--port", "20303", "--ipc", ipc, "--threads", "2", "--chain-id", strconv.FormatInt(chainID, 10), "--accounts", "0", "--silent")
	cmd.Env = append(os.Environ(), "RAYON_NUM_THREADS=2")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start private Anvil fixture: %v", err)
	}
	done := make(chan struct{})
	var processErr error
	go func() { processErr = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	alive := func() {
		select {
		case <-done:
			t.Fatalf("owned private Anvil exited before fixture mutation: %v", processErr)
		default:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var raw *rpc.Client
	for {
		var err error
		alive()
		raw, err = rpc.DialIPC(ctx, ipc)
		if err == nil {
			var cid hexutil.Big
			err = raw.CallContext(ctx, &cid, "eth_chainId")
			if err == nil {
				break
			}
			raw.Close()
		}
		if ctx.Err() != nil {
			t.Fatal("private Anvil startup timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(raw.Close)
	for addr, account := range alloc {
		alive()
		if e := raw.CallContext(ctx, nil, "anvil_setCode", addr, hexutil.Bytes(account.Code)); e != nil {
			t.Fatal(e)
		}
		for key, value := range account.Storage {
			alive()
			if e := raw.CallContext(ctx, nil, "anvil_setStorageAt", addr, key, value); e != nil {
				t.Fatal(e)
			}
		}
	}
	alive()
	if e := raw.CallContext(ctx, nil, "evm_mine"); e != nil {
		t.Fatal(e)
	}
	r := ethclient.NewClient(raw)
	h, e := r.HeaderByNumber(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	block := h.Hash()
	p := Policy{Now: time.Unix(int64(h.Time), 0).Add(time.Second), MaxAge: time.Minute, Timeout: 10 * time.Second}
	return m, r, block, p
}
func hasIssue(a ActivationAttestation, code, role string) bool {
	for _, i := range a.Issues {
		if i.Code == code && (role == "" || i.Role == role) {
			return true
		}
	}
	return false
}
func TestActualEVMIdentityAndCreationPolicy(t *testing.T) {
	m, r, b, p := newEVM(t, nil, 125)
	a, e := Attest(context.Background(), r, m, b, p)
	if e != nil {
		t.Fatal(e)
	}
	if !a.Eligible {
		t.Fatalf("not eligible: %+v", a.Issues)
	}
	if a.CreationFeeRaw != "1000000" || a.USDLDecimals != 6 || !a.Identities["FixturePool"].Verified {
		t.Fatalf("unexpected attestation %+v", a)
	}
	if e = a.ValidateFor(m, p); e != nil {
		t.Fatal(e)
	}
}
func TestIdenticalProxyRuntimeDifferentImplementation(t *testing.T) {
	m, r, b, p := newEVM(t, func(m *Manifest, a types.GenesisAlloc) {
		router := common.HexToAddress(m.Roles["Router"].Address)
		acc := a[router]
		acc.Storage[ImplementationSlot] = addrWord(m.Roles["Quoter"].ImplementationAddress)
		a[router] = acc
	}, 125)
	a, e := Attest(context.Background(), r, m, b, p)
	if e != nil {
		t.Fatal(e)
	}
	if a.Eligible || !hasIssue(a, "implementation_mismatch", "Router") || !hasIssue(a, "unknown_implementation_hash", "Router") {
		t.Fatalf("wrong implementation passed: %+v", a.Issues)
	}
	if a.Identities["Router"].RuntimeHash != a.Identities["Quoter"].RuntimeHash {
		t.Fatal("fixture must have identical proxy code")
	}
}
func TestPendingBeaconGovernanceNeverActivates(t *testing.T) {
	m, r, b, p := newEVM(t, func(m *Manifest, a types.GenesisAlloc) {
		m.Status = "pending-beacon-acceptance"
		addr := common.HexToAddress(m.Roles["PoolBeacon"].Address)
		acc := a[addr]
		acc.Storage[slot(1)] = addrWord("0x1DAe78b53C9F39930a2Ff2B6B7AaC12EC75FB154")
		acc.Storage[slot(2)] = addrWord(m.Governance.ExpectedBeaconOwner)
		a[addr] = acc
	}, 125)
	a, e := Attest(context.Background(), r, m, b, p)
	if e != nil {
		t.Fatal(e)
	}
	if a.Eligible || !hasIssue(a, "governance_handoff_pending", "PoolBeacon") || !hasIssue(a, "manifest_governance_pending", "PoolBeacon") {
		t.Fatalf("pending handoff passed %+v", a)
	}
}
func TestFeeAndTokenDecimalsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, role  string
		slot, value int64
	}{{"fee", "ProtocolConfig", 11, 2000000}, {"decimals", "USDL", 2, 18}} {
		t.Run(tc.name, func(t *testing.T) {
			m, r, b, p := newEVM(t, func(m *Manifest, a types.GenesisAlloc) {
				addr := common.HexToAddress(m.Roles[tc.role].Address)
				acc := a[addr]
				acc.Storage[slot(tc.slot)] = slot(tc.value)
				a[addr] = acc
			}, 125)
			a, e := Attest(context.Background(), r, m, b, p)
			if e != nil {
				t.Fatal(e)
			}
			if a.Eligible || !hasIssue(a, "creation_policy_mismatch", "ProtocolConfig") {
				t.Fatalf("policy mismatch passed %+v", a.Issues)
			}
		})
	}
}
func TestWrongChainStaleAndCancellation(t *testing.T) {
	t.Run("wrong_chain", func(t *testing.T) {
		m, r, b, p := newEVM(t, nil, 1337)
		_, e := Attest(context.Background(), r, m, b, p)
		if e == nil || !strings.Contains(e.Error(), "wrong_chain") {
			t.Fatalf("got %v", e)
		}
	})
	t.Run("stale", func(t *testing.T) {
		m, r, b, p := newEVM(t, nil, 125)
		p.Now = p.Now.Add(2 * time.Minute)
		_, e := Attest(context.Background(), r, m, b, p)
		if e == nil || !strings.Contains(e.Error(), "stale") {
			t.Fatalf("got %v", e)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		m, r, b, p := newEVM(t, nil, 125)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, e := Attest(ctx, r, m, b, p)
		if e == nil {
			t.Fatal("canceled read passed")
		}
	})
}
func TestMalformedSlotAndEmptyCode(t *testing.T) {
	for _, name := range []string{"slot_padding", "empty_impl"} {
		t.Run(name, func(t *testing.T) {
			m, r, b, p := newEVM(t, func(m *Manifest, a types.GenesisAlloc) {
				r := m.Roles["Router"]
				if name == "slot_padding" {
					addr := common.HexToAddress(r.Address)
					acc := a[addr]
					v := addrWord(r.ImplementationAddress)
					v[0] = 1
					acc.Storage[ImplementationSlot] = v
					a[addr] = acc
				} else {
					delete(a, common.HexToAddress(r.ImplementationAddress))
				}
			}, 125)
			a, e := Attest(context.Background(), r, m, b, p)
			if e != nil {
				t.Fatal(e)
			}
			if a.Eligible || a.Identities["Router"].Verified {
				t.Fatal("invalid identity passed")
			}
		})
	}
}

func TestActualEVMWrongProtocolSettlementBlocksTradingAndQuote(t *testing.T) {
	m, r, b, p := newEVM(t, func(m *Manifest, a types.GenesisAlloc) {
		addr := common.HexToAddress(m.Roles["ProtocolConfig"].Address)
		acc := a[addr]
		acc.Storage[slot(1)] = addrWord("0x2222222222222222222222222222222222222222")
		a[addr] = acc
	}, 125)
	a, e := Attest(t.Context(), r, m, b, p)
	if e != nil {
		t.Fatal(e)
	}
	if a.Eligible || a.SettlementAddress == SettlementAddress {
		t.Fatal("wrong observed settlement accepted")
	}
	if e = a.ValidateFamilyFor(m, p, []string{"Router", "USDL", "ProtocolConfig"}, false); e == nil {
		t.Fatal("wrong observed token enabled settlement family")
	}
	if e = a.ValidateFamilyFor(m, p, []string{"FeesRouter", "SidioraNFT", "FeeAccumulator"}, false); e != nil {
		t.Fatalf("unrelated family blocked: %v", e)
	}
}
