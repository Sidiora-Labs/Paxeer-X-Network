package main

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func erc20Call(t *testing.T, parsed *abi.ABI, token common.Address, name string, value *big.Int, args ...interface{}) *callReq {
	t.Helper()
	m, ok := parsed.Methods[name]
	if !ok {
		t.Fatalf("SidioraERC20 ABI has no %s", name)
	}
	input, err := parsed.Pack(name, args...)
	if err != nil {
		t.Fatalf("pack %s: %v", name, err)
	}
	output, err := m.Outputs.Pack(value)
	if err != nil {
		t.Fatalf("pack %s output: %v", name, err)
	}
	return &callReq{method: &m, rec: &callRecord{
		Key: roleSidioraERC20 + "." + m.RawName, Signature: m.Sig, Selector: hexutil.Bytes(m.ID),
		To: token, Input: input, Output: output,
	}}
}

func TestRecordRefusesSupplyMismatch(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../precompiles/launchpad/abi/SidioraERC20.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	token := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	pool := goldenGeneration2Pool
	alice := common.HexToAddress("0x00000000000000000000000000000000000b0001")
	bob := common.HexToAddress("0x00000000000000000000000000000000000b0002")
	holders := []common.Address{pool, alice, bob}
	supply := erc20Call(t, &parsed, token, "totalSupply", big.NewInt(1_000_000))
	balances := func(values ...int64) []*callReq {
		out := make([]*callReq, len(values))
		for i, v := range values {
			out[i] = erc20Call(t, &parsed, token, "balanceOf", big.NewInt(v), holders[i])
		}
		return out
	}

	rec, err := supplyRecord(token, pool, holders, supply, balances(995_000, 4_000, 1_000))
	if err != nil {
		t.Fatalf("matching supply refused: %v", err)
	}
	if rec.Token != token || rec.Pool != pool || rec.Holders != 3 ||
		rec.TotalSupply.ToInt().Cmp(big.NewInt(1_000_000)) != 0 || rec.HolderSum.ToInt().Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatalf("supply record = %+v", rec)
	}

	_, err = supplyRecord(token, pool, holders, supply, balances(995_000, 4_000, 999))
	if !errors.Is(err, errSupplyMismatch) {
		t.Fatalf("short holder sum: error %v, want %v", err, errSupplyMismatch)
	}
	want := "token " + token.Hex() + ": pool-token holder sum differs from totalSupply: holders 999999, totalSupply 1000000"
	if err.Error() != want {
		t.Fatalf("short holder sum: error %q, want %q", err, want)
	}

	_, err = supplyRecord(token, pool, holders, supply, balances(995_000, 4_000, 1_001))
	want = "token " + token.Hex() + ": pool-token holder sum differs from totalSupply: holders 1000001, totalSupply 1000000"
	if !errors.Is(err, errSupplyMismatch) || err.Error() != want {
		t.Fatalf("long holder sum: error %v, want %q", err, want)
	}

	_, err = supplyRecord(token, pool, []common.Address{pool, alice, alice}, supply, balances(995_000, 4_000, 1_000))
	want = "token " + token.Hex() + ": holder " + alice.Hex() + " listed twice"
	if err == nil || err.Error() != want {
		t.Fatalf("duplicate holder: error %v, want %q", err, want)
	}

	reverted := balances(995_000, 4_000, 1_000)
	reverted[2].rec.Reverted = true
	reverted[2].rec.Output = nil
	_, err = supplyRecord(token, pool, holders, supply, reverted)
	want = "SidioraERC20.balanceOf at " + token.Hex() + " reverted"
	if err == nil || err.Error() != want {
		t.Fatalf("reverted balance: error %v, want %q", err, want)
	}

	_, err = supplyRecord(token, pool, holders, supply, balances(995_000, 5_000))
	want = "token " + token.Hex() + ": 3 holders and 2 balance reads"
	if err == nil || err.Error() != want {
		t.Fatalf("missing balance read: error %v, want %q", err, want)
	}
}

type planFixture struct {
	in     *planInputs
	abis   map[string]*abi.ABI
	gen1   *generation
	gen2   *generation
	trader common.Address
}

func loadPlanFixture(t *testing.T) *planFixture {
	t.Helper()
	abis, err := loadABIs("../../../../../../precompiles/launchpad/abi")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := loadLayout("../layout.json")
	if err != nil {
		t.Fatal(err)
	}
	if layout.pendingGuardian != 25 || layout.feeStrategy != 12 ||
		layout.pausable != common.HexToHash("0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258") {
		t.Fatalf("layout slots = %+v", layout)
	}
	holders, err := loadHolders("../holders.json")
	if err != nil {
		t.Fatal(err)
	}
	gens := generations()
	gen1, gen2 := gens[0], gens[1]
	gen1Other := common.HexToAddress("0x00000000000000000000000000000000000c0001")
	gen2Other := common.HexToAddress("0x00000000000000000000000000000000000c0002")
	gen1.pools = []common.Address{gen1.goldenPool, gen1Other}
	gen2.pools = []common.Address{gen2Other, gen2.goldenPool}
	gen1.treasury = common.HexToAddress("0x00000000000000000000000000000000000d0001")
	gen2Token := common.HexToAddress("0x0F92FDD087c96dD3413eF98646b94D0d986192FA")
	poolToken := map[common.Address]common.Address{
		gen1.goldenPool: common.HexToAddress("0xA19EC83e0e74c3eD7A30d5f83F08A541375c9a5c"),
		gen1Other:       common.HexToAddress("0x00000000000000000000000000000000000a0011"),
		gen2.goldenPool: gen2Token,
		gen2Other:       common.HexToAddress("0x00000000000000000000000000000000000a0012"),
	}
	trader := common.HexToAddress("0x00000000000000000000000000000000000b0001")
	return &planFixture{
		abis: abis, gen1: gen1, gen2: gen2, trader: trader,
		in: &planInputs{
			trader:         trader,
			buyAmount:      big.NewInt(1_000_000),
			sellAmount:     big.NewInt(1_000_000_000_000),
			deadline:       big.NewInt(1_700_003_600),
			gens:           gens,
			poolToken:      poolToken,
			layout:         layout,
			creationFee:    big.NewInt(5_000_000),
			guardian:       common.HexToAddress("0x00000000000000000000000000000000000e0001"),
			paused:         false,
			feeRight:       big.NewInt(7),
			feeRightOwner:  common.HexToAddress("0x00000000000000000000000000000000000e0002"),
			feeStrategy:    strategyAirdrop,
			optical:        common.HexToAddress("0x00000000000000000000000000000000000e0003"),
			opticalSurplus: big.NewInt(0),
			airdropEpoch:   big.NewInt(3),
			tokenHolders:   holders[gen2Token],
		},
	}
}

type wantTrace struct {
	family, key, sig string
	to, from         common.Address
	pre              stateOverride
}

func TestCallerPlanCoversAllFamilies(t *testing.T) {
	f := loadPlanFixture(t)
	in, g1, g2, trader := f.in, f.gen1, f.gen2, f.trader
	p := g2.goldenPool
	holder := common.HexToAddress("0x1DAe78b53C9F39930a2Ff2B6B7AaC12EC75FB154")
	owner := in.feeRightOwner
	word := func(n int64) common.Hash { return common.BigToHash(big.NewInt(n)) }
	strategySlot := crypto.Keccak256Hash(common.LeftPadBytes([]byte{7}, 32), common.LeftPadBytes([]byte{12}, 32))
	strategyPre := func(n int64) stateOverride {
		return stateOverride{g2.collection: {StateDiff: map[common.Hash]common.Hash{strategySlot: word(n)}}}
	}
	permitSig := "(uint256,uint256,uint8,bytes32,bytes32))"
	want := []wantTrace{}
	for _, g := range []*generation{g1, g2} {
		want = append(want,
			wantTrace{familySwap, "Router.buy", "buy(address,uint256,uint256,uint256)", g.router, trader, nil},
			wantTrace{familySwap, "Router.sell", "sell(address,uint256,uint256,uint256)", g.router, trader, nil},
			wantTrace{familySwap, "Router.swapTokenForToken", "swapTokenForToken(address,address,uint256,uint256,uint256)", g.router, trader, nil},
			wantTrace{familySwap, "SidioraPool.swap", "swap(uint256,uint256,bool,address,uint256)", g.goldenPool, trader, nil},
			wantTrace{familySwap, "SidioraPool.swap", "swap(uint256,uint256,bool,address,uint256)", g.goldenPool, trader, nil},
		)
	}
	want = append(want,
		wantTrace{familyPermit, "Router.buyWithPermit", "buyWithPermit(address,uint256,uint256,uint256," + permitSig, g2.router, trader, nil},
		wantTrace{familyPermit, "Router.sellWithPermit", "sellWithPermit(address,uint256,uint256,uint256," + permitSig, g2.router, trader, nil},
		wantTrace{familyPermit, "Router.swapTokenForTokenWithPermit", "swapTokenForTokenWithPermit(address,address,uint256,uint256,uint256," + permitSig, g2.router, trader, nil},
		wantTrace{familyPermit, "Router.createMarketWithPermit", "createMarketWithPermit(string,string,uint8,address," + permitSig, g2.router, trader, nil},
		wantTrace{familyGuardian, "SidioraPool.transferGuardian", "transferGuardian(address)", p, in.guardian, nil},
		wantTrace{familyGuardian, "SidioraPool.acceptGuardian", "acceptGuardian()", p, trader,
			stateOverride{p: {StateDiff: map[common.Hash]common.Hash{word(25): common.BytesToHash(trader.Bytes())}}}},
		wantTrace{familyGuardian, "SidioraPool.pause", "pause()", p, in.guardian, nil},
		wantTrace{familyGuardian, "SidioraPool.unpause", "unpause()", p, in.guardian,
			stateOverride{p: {StateDiff: map[common.Hash]common.Hash{in.layout.pausable: word(1)}}}},
		wantTrace{familyNFT, "SidioraNFT.approve", "approve(address,uint256)", g2.collection, owner, nil},
		wantTrace{familyNFT, "SidioraNFT.setApprovalForAll", "setApprovalForAll(address,bool)", g2.collection, owner, nil},
		wantTrace{familyNFT, "SidioraNFT.transferFrom", "transferFrom(address,address,uint256)", g2.collection, owner, nil},
		wantTrace{familyNFT, "SidioraNFT.safeTransferFrom", "safeTransferFrom(address,address,uint256)", g2.collection, owner, nil},
		wantTrace{familyNFT, "SidioraNFT.safeTransferFrom", "safeTransferFrom(address,address,uint256,bytes)", g2.collection, owner, nil},
		wantTrace{familyNFT, "SidioraNFT.setFeeStrategy", "setFeeStrategy(uint256,uint8)", g2.collection, owner, nil},
		wantTrace{familyNFT, "SidioraNFT.mint", "mint(address,address,uint8)", g2.collection, g2.factory, nil},
		wantTrace{familyFeeClaim, "FeesRouter.setFeeStrategy", "setFeeStrategy(uint256,uint8)", g2.feesRouter, owner, nil},
		wantTrace{familyFeeClaim, "FeesRouter.claimFees", "claimFees(uint256)", g2.feesRouter, owner, strategyPre(0)},
		wantTrace{familyFeeClaim, "FeesRouter.executeBurn", "executeBurn(uint256)", g2.feesRouter, owner, strategyPre(1)},
		wantTrace{familyFeeClaim, "FeesRouter.executeAirdrop", "executeAirdrop(uint256)", g2.feesRouter, owner, nil},
		wantTrace{familyFeeClaim, "FeesRouter.executeLpRewards", "executeLpRewards(uint256)", g2.feesRouter, owner, strategyPre(3)},
		wantTrace{familyFeeClaim, "FeesRouter.claimAirdrop", "claimAirdrop(uint256)", g2.feesRouter, holder, nil},
		wantTrace{familyFeeClaim, "FeesRouter.claimAirdropEpoch", "claimAirdropEpoch(uint256,uint256)", g2.feesRouter, holder, nil},
		wantTrace{familyFeeClaim, "FeeAccumulator.claim", "claim(address,address)", g2.feeAccumulator, g2.feesRouter, nil},
		wantTrace{familyFeeClaim, "FeeAccumulator.claimAirdrop", "claimAirdrop(address,address)", g2.feeAccumulator, holder, nil},
		wantTrace{familyFeeClaim, "FeeAccumulator.claimAirdropEpoch", "claimAirdropEpoch(address,address,uint256)", g2.feeAccumulator, holder, nil},
		wantTrace{familyFeeClaim, "FeeAccumulator.claimLegacyAirdrop", "claimLegacyAirdrop(address,uint256,address,uint256,bytes32[])", g2.feeAccumulator, holder, nil},
		wantTrace{familyFeeClaim, "FeeAccumulator.claimOpticalSurplus", "claimOpticalSurplus(address,uint256,address)", g2.feeAccumulator, in.optical, nil},
	)

	plan, err := buildTracePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != len(want) {
		t.Fatalf("plan has %d entries, want %d", len(plan), len(want))
	}
	families := map[string]int{}
	inputs := map[string][]byte{}
	for i, p := range plan {
		w := want[i]
		m, input, err := packPlan(f.abis, p)
		if err != nil {
			t.Fatalf("entry %d %s: %v", i, w.sig, err)
		}
		key := p.Role + "." + m.RawName
		if p.Family != w.family || key != w.key || m.Sig != w.sig || p.Sig != w.sig || p.To != w.to || p.From != w.from {
			t.Fatalf("entry %d = %s %s %s to %s from %s, want %s %s %s to %s from %s",
				i, p.Family, key, m.Sig, p.To.Hex(), p.From.Hex(), w.family, w.key, w.sig, w.to.Hex(), w.from.Hex())
		}
		if !reflect.DeepEqual(p.PreState, w.pre) {
			t.Fatalf("entry %d %s pre-state = %v, want %v", i, w.sig, p.PreState, w.pre)
		}
		if !bytes.Equal(input[:4], m.ID) {
			t.Fatalf("entry %d %s selector %x, want %x", i, w.sig, input[:4], m.ID)
		}
		families[p.Family]++
		inputs[w.family+" "+w.sig] = input
	}
	wantFamilies := map[string]int{familySwap: 10, familyPermit: 4, familyGuardian: 4, familyNFT: 7, familyFeeClaim: 12}
	if !reflect.DeepEqual(families, wantFamilies) {
		t.Fatalf("families = %v, want %v", families, wantFamilies)
	}

	args := func(role, sig string, input []byte) []interface{} {
		m, err := methodBySig(f.abis[role], role, sig)
		if err != nil {
			t.Fatal(err)
		}
		out, err := m.Inputs.Unpack(input[4:])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	buy := args(roleRouter, want[10].sig, inputs[familyPermit+" "+want[10].sig])
	permit := reflect.ValueOf(buy[4])
	if buy[0] != p || buy[1].(*big.Int).Cmp(in.buyAmount) != 0 || buy[3].(*big.Int).Cmp(in.deadline) != 0 ||
		permit.FieldByName("Value").Interface().(*big.Int).Cmp(in.buyAmount) != 0 ||
		permit.FieldByName("Deadline").Interface().(*big.Int).Cmp(in.deadline) != 0 ||
		permit.FieldByName("V").Interface() != uint8(0) || permit.FieldByName("R").Interface() != [32]byte{} ||
		permit.FieldByName("S").Interface() != [32]byte{} {
		t.Fatalf("buyWithPermit args = %v", buy)
	}
	create := args(roleRouter, want[13].sig, inputs[familyPermit+" "+want[13].sig])
	if create[0] != traceMarketName || create[1] != traceMarketSymbol || create[2] != uint8(0) || create[3] != (common.Address{}) {
		t.Fatalf("createMarketWithPermit args = %v", create)
	}
	transfer := args(roleSidioraNFT, "transferFrom(address,address,uint256)", inputs[familyNFT+" transferFrom(address,address,uint256)"])
	if transfer[0] != owner || transfer[1] != trader || transfer[2].(*big.Int).Int64() != 7 {
		t.Fatalf("transferFrom args = %v", transfer)
	}
	strategy := args(roleSidioraNFT, "setFeeStrategy(uint256,uint8)", inputs[familyNFT+" setFeeStrategy(uint256,uint8)"])
	if strategy[1] != strategyClaim {
		t.Fatalf("setFeeStrategy from strategy %d sets %v, want %d", in.feeStrategy, strategy[1], strategyClaim)
	}
	mint := args(roleSidioraNFT, "mint(address,address,uint8)", inputs[familyNFT+" mint(address,address,uint8)"])
	if mint[0] != trader || mint[1] != p || mint[2] != uint8(0) {
		t.Fatalf("mint args = %v", mint)
	}
	claim := args(roleFeeAccumulator, "claim(address,address)", inputs[familyFeeClaim+" claim(address,address)"])
	if claim[0] != p || claim[1] != owner {
		t.Fatalf("claim args = %v", claim)
	}
	epoch := args(roleFeeAccumulator, "claimAirdropEpoch(address,address,uint256)", inputs[familyFeeClaim+" claimAirdropEpoch(address,address,uint256)"])
	if epoch[0] != p || epoch[1] != holder || epoch[2].(*big.Int).Int64() != 3 {
		t.Fatalf("claimAirdropEpoch args = %v", epoch)
	}
	surplus := args(roleFeeAccumulator, "claimOpticalSurplus(address,uint256,address)", inputs[familyFeeClaim+" claimOpticalSurplus(address,uint256,address)"])
	if surplus[0] != p || surplus[1].(*big.Int).Int64() != 1 || surplus[2] != trader {
		t.Fatalf("claimOpticalSurplus args = %v", surplus)
	}

	in.paused = true
	in.pendingGuardian = common.HexToAddress("0x00000000000000000000000000000000000e0004")
	in.feeStrategy = strategyClaim
	in.opticalSurplus = big.NewInt(42)
	plan, err = buildTracePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]tracePlan{}
	for _, p := range plan {
		byKey[p.Family+" "+p.Sig] = p
	}
	accept := byKey[familyGuardian+" acceptGuardian()"]
	if accept.From != in.pendingGuardian || accept.PreState != nil {
		t.Fatalf("acceptGuardian with a pending guardian = from %s pre-state %v", accept.From.Hex(), accept.PreState)
	}
	pause := byKey[familyGuardian+" pause()"]
	wantPause := stateOverride{p: {StateDiff: map[common.Hash]common.Hash{in.layout.pausable: {}}}}
	if !reflect.DeepEqual(pause.PreState, wantPause) || byKey[familyGuardian+" unpause()"].PreState != nil {
		t.Fatalf("paused pool: pause pre-state %v, unpause pre-state %v", pause.PreState, byKey[familyGuardian+" unpause()"].PreState)
	}
	if byKey[familyFeeClaim+" claimFees(uint256)"].PreState != nil ||
		!reflect.DeepEqual(byKey[familyFeeClaim+" executeAirdrop(uint256)"].PreState, strategyPre(2)) {
		t.Fatal("claim strategy pre-state does not follow the recorded strategy")
	}
	if got := byKey[familyNFT+" setFeeStrategy(uint256,uint8)"].Args[1]; got != strategyBurn {
		t.Fatalf("setFeeStrategy from strategy 0 sets %v, want %d", got, strategyBurn)
	}
	if got := byKey[familyFeeClaim+" claimOpticalSurplus(address,uint256,address)"].Args[1].(*big.Int).Int64(); got != 42 {
		t.Fatalf("claimOpticalSurplus amount %d, want 42", got)
	}
}

func TestCallerPlanRefusesMissingCallers(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(f *planFixture)
		error string
	}{
		{"no guardian", func(f *planFixture) { f.in.guardian = common.Address{} },
			"pool " + goldenGeneration2Pool.Hex() + " has no guardian"},
		{"no fee right", func(f *planFixture) { f.in.feeRight = new(big.Int) },
			"pool " + goldenGeneration2Pool.Hex() + " has no fee right in collection 0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"},
		{"no fee right owner", func(f *planFixture) { f.in.feeRightOwner = common.Address{} },
			"fee right 7 of pool " + goldenGeneration2Pool.Hex() + " has no owner"},
		{"no airdrop holder", func(f *planFixture) { f.in.tokenHolders = []common.Address{goldenGeneration2Pool, deadAddress} },
			"token 0x0F92FDD087c96dD3413eF98646b94D0d986192FA of pool " + goldenGeneration2Pool.Hex() + " has no holder that can claim an airdrop"},
		{"one pool", func(f *planFixture) { f.gen1.pools = f.gen1.pools[:1] },
			"generation 1 lists 1 pools, a token-for-token trace needs two"},
	}
	for _, c := range cases {
		f := loadPlanFixture(t)
		c.edit(f)
		plan, err := buildTracePlan(f.in)
		if err == nil || err.Error() != c.error || plan != nil {
			t.Fatalf("%s: plan %d entries, error %v, want %q", c.name, len(plan), err, c.error)
		}
	}
}
