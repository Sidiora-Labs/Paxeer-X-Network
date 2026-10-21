package coverage

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"testing"
)

func TestNativeTransfersAndMirrorsReplayOnceForActualRecipient(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProjection(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	holder := common.HexToAddress("0xa1")
	recipient := common.HexToAddress("0xb1")
	token := addresses["SidioraERC20"]
	mint := eventLog(t, token, abis["SidioraERC20"].Events["Transfer"], map[string]any{"to": holder, "value": big.NewInt(100)})
	transfer := eventLog(t, token, abis["SidioraERC20"].Events["Transfer"], map[string]any{"from": holder, "to": recipient, "value": big.NewInt(40)})
	mirror := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["TokenTransfer"], map[string]any{"token": token, "from": holder, "to": recipient, "value": big.NewInt(40)})
	out := correlateInput(t, registry, mint, transfer, mirror)
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if p.Balances[token.Hex()+recipient.Hex()].Cmp(big.NewInt(40)) != 0 || p.Balances[token.Hex()+holder.Hex()].Cmp(big.NewInt(60)) != 0 || p.Supply[token].Cmp(big.NewInt(100)) != 0 {
		t.Fatal("native amount duplicated")
	}
	from := out.Effects[0].Actors.TxFrom.Address
	if from == recipient || p.Balances[token.Hex()+from.Hex()] != nil {
		t.Fatal("transaction origin received holder balance")
	}
	out.Effects[1].Fields["value"] = big.NewInt(1000)
	if err = p.ApplyReceipt(out); err == nil {
		t.Fatal("modified correlated economic evidence admitted")
	}
}
func TestNativeClaimMirrorReplaySettlesLiabilityOnce(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProjection(lifecycle)
	pool := addresses["SidioraPool"]
	recipient := common.HexToAddress("0xb1")
	record := eventLog(t, addresses["FeeAccumulator"], abis["FeeAccumulator"].Events["FeeRecorded"], map[string]any{"pool": pool, "feeAmount": big.NewInt(20), "protocolCut": big.NewInt(10), "poolCut": big.NewInt(10)})
	claim := eventLog(t, addresses["FeeAccumulator"], abis["FeeAccumulator"].Events["FeesClaimed"], map[string]any{"pool": pool, "recipient": recipient, "amount": big.NewInt(10)})
	mirror := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["FeeFlow"], map[string]any{"kind": uint8(1), "pool": pool, "party": recipient, "amount": big.NewInt(10)})
	out := correlateInput(t, registry, record, claim, mirror)
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if p.CreatorLiability[pool].Sign() != 0 || len(p.Claims) != 1 {
		t.Fatal("claim did not settle once")
	}
}
func TestNativeDepositReserveAndMirrorDoNotDuplicateFunding(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProjection(lifecycle)
	vault := addresses["PECORVault"]
	token := addresses["SidioraERC20"]
	user := common.HexToAddress("0xa1")
	deposit := eventLog(t, vault, abis["PECORVault"].Events["Deposit"], map[string]any{"token": token, "depositor": user, "amount": big.NewInt(7), "newReserve": big.NewInt(7)})
	update := eventLog(t, vault, abis["PECORVault"].Events["ReservesUpdated"], map[string]any{"token": token, "oldReserve": big.NewInt(0), "newReserve": big.NewInt(7)})
	mirror := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["VaultFlow"], map[string]any{"flowType": uint8(0), "token": token, "party": user, "amount": big.NewInt(7), "newReserve": big.NewInt(7)})
	out := correlateInput(t, registry, deposit, update, mirror)
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if p.Funding[vault.Hex()+token.Hex()+user.Hex()].Cmp(big.NewInt(7)) != 0 || p.Reserves[vault.Hex()+token.Hex()].Cmp(big.NewInt(7)) != 0 {
		t.Fatal("funding/reserves duplicated")
	}
	found := false
	for _, d := range out.Diagnostics {
		if d == "vault_reserve_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatal("getter unavailable falsely attested")
	}
}
func TestInterruptedProjectionDoesNotPublishPartialEconomics(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProjection(lifecycle)
	owner := common.HexToAddress("0xa1")
	token := addresses["SidioraERC20"]
	mint := eventLog(t, token, abis["SidioraERC20"].Events["Transfer"], map[string]any{"to": owner, "value": big.NewInt(5)})
	invalid := eventLog(t, token, abis["SidioraERC20"].Events["Transfer"], map[string]any{"from": owner, "to": common.HexToAddress("0xb1"), "value": big.NewInt(6)})
	if err = p.ApplyReceipt(correlateInput(t, registry, mint, invalid)); err == nil {
		t.Fatal("missing balance accepted")
	}
	if len(p.Balances) != 0 || len(p.seen) != 0 || len(p.Supply) != 0 {
		t.Fatal("partial receipt committed")
	}
}

func TestNFTNativeMintAndBurnKeepOwnershipWithoutMirrorDuplication(t *testing.T) {
	registry, abis, addresses := sourceRegistry(t)
	lifecycle, err := NewLifecycle(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProjection(lifecycle)
	nft := addresses["SidioraNFT"]
	owner := common.HexToAddress("0xa1")
	id := big.NewInt(9)
	mint := eventLog(t, nft, abis["SidioraNFT"].Events["Transfer"], map[string]any{"to": owner, "tokenId": id})
	mirror := eventLog(t, addresses["EventEmitter"], abis["EventEmitter"].Events["NftTransfer"], map[string]any{"nft": nft, "to": owner, "tokenId": id})
	out := correlateInput(t, registry, mint, mirror)
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if err = p.ApplyReceipt(out); err != nil {
		t.Fatal(err)
	}
	if p.NFTOwners[nft.Hex()+"/9"] != owner {
		t.Fatal("native NFT owner lost")
	}
	burn := eventLog(t, nft, abis["SidioraNFT"].Events["Transfer"], map[string]any{"from": owner, "tokenId": id})
	if err = p.ApplyReceipt(correlateInput(t, registry, burn)); err != nil {
		t.Fatal(err)
	}
	if len(p.NFTOwners) != 0 {
		t.Fatal("burn retained ownership")
	}
}
