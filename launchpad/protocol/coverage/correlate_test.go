package coverage

import (
	"bytes"
	"context"
	"math/big"
	"reflect"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/catalog"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func sourceRegistry(t *testing.T) (*catalog.Registry, map[string]abi.ABI, map[string]common.Address) {
	t.Helper()
	bundles, err := catalog.Bundles()
	if err != nil {
		t.Fatal(err)
	}
	deployments := []catalog.Deployment{}
	abis := map[string]abi.ABI{}
	addresses := map[string]common.Address{}
	for i, b := range bundles {
		a, err := abi.JSON(bytes.NewReader(b.ABI))
		if err != nil {
			t.Fatal(err)
		}
		address := common.BigToAddress(big.NewInt(int64(i + 1)))
		addresses[b.Name] = address
		abis[b.Name] = a
		deployments = append(deployments, catalog.Deployment{ChainID: 125, Address: address, Family: b.Name, Version: b.ABIHash, Attested: true})
	}
	registry, err := catalog.New(deployments)
	if err != nil {
		t.Fatal(err)
	}
	return registry, abis, addresses
}
func eventLog(t *testing.T, address common.Address, event abi.Event, fields map[string]any) types.Log {
	t.Helper()
	topics := []common.Hash{event.ID}
	values := []any{}
	for _, input := range event.Inputs {
		v, ok := fields[input.Name]
		if !ok {
			v = reflect.Zero(input.Type.GetType()).Interface()
			if input.Type.GetType() == reflect.TypeOf((*big.Int)(nil)) {
				v = big.NewInt(0)
			}
		}
		if input.Indexed {
			packed, err := (abi.Arguments{{Type: input.Type}}).Pack(v)
			if err != nil {
				t.Fatal(err)
			}
			if len(packed) != 32 {
				t.Fatal("fixture requires fixed indexed type")
			}
			topics = append(topics, common.BytesToHash(packed))
		} else {
			values = append(values, v)
		}
	}
	data, err := event.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	return types.Log{Address: address, Topics: topics, Data: data}
}
func receiptInput(t *testing.T, logs ...types.Log) (*types.Header, *types.Transaction, *types.Receipt) {
	return receiptInputAt(t, 12, logs...)
}
func receiptInputAt(t *testing.T, block uint64, logs ...types.Log) (*types.Header, *types.Transaction, *types.Receipt) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	target := common.HexToAddress("0x1234")
	tx, err := types.SignTx(types.NewTx(&types.LegacyTx{To: &target, Gas: 21000, GasPrice: big.NewInt(1)}), types.LatestSignerForChainID(big.NewInt(125)), key)
	if err != nil {
		t.Fatal(err)
	}
	header := &types.Header{Number: new(big.Int).SetUint64(block), Time: 100, GasLimit: 1000000}
	receipt := &types.Receipt{TxHash: tx.Hash(), BlockHash: header.Hash(), BlockNumber: header.Number, Status: types.ReceiptStatusSuccessful}
	for i := range logs {
		l := logs[i]
		l.Index = uint(i)
		l.BlockNumber = block
		l.TxHash = tx.Hash()
		l.BlockHash = header.Hash()
		receipt.Logs = append(receipt.Logs, &l)
	}
	return header, tx, receipt
}
func correlateInput(t *testing.T, registry *catalog.Registry, logs ...types.Log) *ReceiptResult {
	t.Helper()
	header, tx, receipt := receiptInput(t, logs...)
	out, err := (&Correlator{Registry: registry}).Correlate(context.Background(), header, tx, receipt)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestUnauthorizedOriginsAndGenericBusinessNamesHaveZeroEffects(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	unauthorized := eventLog(t, common.HexToAddress("0xffff"), abis["FeeAccumulator"].Events["FeesClaimed"], map[string]any{"pool": addresses["SidioraPool"], "recipient": common.HexToAddress("0xab"), "amount": big.NewInt(100)})
	generic := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["EventLog"], map[string]any{"msgSender": common.HexToAddress("0xffff"), "eventName": "FeeClaimed", "eventNameHash": [32]byte(crypto.Keccak256Hash([]byte("FeeClaimed")))})
	out := correlateInput(t, registry, unauthorized, generic)
	if len(out.Effects) != 0 || out.Records[0].Status != "origin_unrecognized" || out.Records[1].Status != "schema_unrecognized" {
		t.Fatalf("untrusted economics: %+v", out)
	}
}
func TestSuccessfulEngineReceiptSurvivesTrackerFailure(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	swap := eventLog(t, addresses["PECOR"], abis["PECOR"].Events["SimpleSwap"], map[string]any{"amountIn": big.NewInt(9), "amountOut": big.NewInt(8), "executionPrice": big.NewInt(1)})
	failure := eventLog(t, addresses["PECOR"], abis["PECOR"].Events["TrackerCallFailed"], map[string]any{})
	out := correlateInput(t, registry, swap, failure)
	if !out.Successful || !out.EnrichmentMissing {
		t.Fatal("native execution truth lost")
	}
	executions := 0
	for _, e := range out.Effects {
		if e.Event == "SimpleSwap" {
			executions++
		}
	}
	if executions != 1 {
		t.Fatal("engine execution duplicated", executions)
	}
}
func TestReceiptDuplicateAndConflictingIdentities(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	l := eventLog(t, addresses["SidioraERC20"], abis["SidioraERC20"].Events["Transfer"], map[string]any{"value": big.NewInt(4)})
	h, tx, r := receiptInput(t, l)
	copyLog := *r.Logs[0]
	r.Logs = append(r.Logs, &copyLog)
	c := &Correlator{Registry: registry}
	out, err := c.Correlate(context.Background(), h, tx, r)
	if err != nil || len(out.Records) != 1 {
		t.Fatal("duplicate raw not collapsed", err)
	}
	copyLog.Data = append([]byte(nil), copyLog.Data...)
	copyLog.Data[31] = 5
	if _, err = c.Correlate(context.Background(), h, tx, r); err == nil {
		t.Fatal("conflicting raw identity accepted")
	}
}
func TestWrongTransactionAndBlockCannotAttachCreation(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	l := eventLog(t, addresses["SidioraFactory"], abis["SidioraFactory"].Events["MarketCreated"], map[string]any{})
	h, tx, r := receiptInput(t, l)
	r.Logs[0].TxHash = common.HexToHash("0xff")
	if _, err := (&Correlator{Registry: registry}).Correlate(context.Background(), h, tx, r); err == nil {
		t.Fatal("foreign creation receipt attached")
	}
}
func TestDirectSwapWithoutCallerProofReportsCoverageGap(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	l := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["Swap"], map[string]any{})
	out := correlateInput(t, registry, l)
	if len(out.Effects) != 0 || out.Records[0].Status != "caller_evidence_unavailable" || CoreSwapCoverage(false) != "coverage_gap_direct_swaps" {
		t.Fatal("unattested mirror manufactured fill")
	}
}

func TestSameCreatorCreationReceiptsKeepExactPoolAndHash(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	creator := common.HexToAddress("0xc1")
	pools := []common.Address{common.HexToAddress("0xd1"), common.HexToAddress("0xd2")}
	hashes := []common.Hash{}
	for _, pool := range pools {
		l := eventLog(t, addresses["SidioraFactory"], abis["SidioraFactory"].Events["MarketCreated"], map[string]any{"creator": creator, "pool": pool, "token": addresses["SidioraERC20"], "nftId": big.NewInt(1)})
		out := correlateInput(t, registry, l)
		if len(out.Records) != 1 || out.Records[0].Decoded.Fields["pool"] != pool || out.Records[0].Decoded.Fields["creator"] != creator {
			t.Fatal("creation identity crossed receipt")
		}
		hashes = append(hashes, out.TransactionHash)
		if _, err := out.ResolveCreation(out.TransactionHash); err == nil {
			t.Fatal("unverified registry creation resolved")
		}
	}
	if hashes[0] == hashes[1] {
		t.Fatal("creation receipts share hash")
	}
}

func TestOpticalMirrorMismatchCannotChangeCreatorLiability(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	pool := addresses["SidioraPool"]
	native := eventLog(t, addresses["FeeAccumulator"], abis["FeeAccumulator"].Events["OpticalSurplusRecorded"], map[string]any{"pool": pool, "amount": big.NewInt(5)})
	mirror := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["FeeFlow"], map[string]any{"kind": uint8(7), "pool": pool, "amount": big.NewInt(6)})
	out := correlateInput(t, registry, native, mirror)
	if len(out.Effects) != 1 || companionMatches(out.Effects[0], out.Records[1]) {
		t.Fatal("inconsistent optical mirror accepted")
	}
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	projection, _ := NewProjection(lifecycle)
	if err = projection.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if projection.OpticalLiability[pool].Cmp(big.NewInt(5)) != 0 || projection.CreatorLiability[pool] != nil {
		t.Fatal("optical amount mixed with creator funds")
	}
}
