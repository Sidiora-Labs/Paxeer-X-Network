package main

import (
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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
