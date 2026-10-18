package feeds

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
)

// spotLog packs the values of event name of the leaf ABI load, in signature
// order, as log index of block n at address.
func spotLog(t *testing.T, load func() (abi.ABI, error), name string, address common.Address, n uint64, index uint, kind decode.ReceiptKind,
	from *common.Address, at time.Time, values ...any) decode.Log {
	t.Helper()
	a, err := load()
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := a.Events[name]
	if !ok {
		t.Fatalf("leaf ABI declares no event %s", name)
	}
	topics := []common.Hash{ev.ID}
	var data []any
	for i, in := range ev.Inputs {
		if !in.Indexed {
			data = append(data, values[i])
			continue
		}
		hashes, err := abi.MakeTopics([]any{values[i]})
		if err != nil {
			t.Fatalf("%s topic %s: %v", name, in.Name, err)
		}
		topics = append(topics, hashes[0][0])
	}
	packed, err := ev.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		t.Fatalf("%s pack: %v", name, err)
	}
	return decode.Log{
		Log: ethtypes.Log{Address: address, Topics: topics, Data: packed, BlockNumber: n, BlockHash: chainBlock(n).Hash,
			TxHash: crypto.Keccak256Hash([]byte("tx"), big.NewInt(int64(n)).Bytes()), Index: index},
		ReceiptKind: kind,
		TxFrom:      from,
		BlockTime:   at,
	}
}

func chainBlock(n uint64) store.Block {
	return store.Block{
		Number:     n,
		Hash:       crypto.Keccak256Hash([]byte("block"), big.NewInt(int64(n)).Bytes()),
		ParentHash: crypto.Keccak256Hash([]byte("block"), big.NewInt(int64(n-1)).Bytes()),
		Time:       time.Now().UTC().Add(-time.Duration(10-n) * time.Second),
		Proposer:   "AB",
		TxCount:    1,
	}
}

// TestTopicsSpotSaleWakesBaseTape decodes a SimpleSwap that sells SID into
// USDL, writes it with store.WriteBlock and feeds every outbox message of its
// block to Message: the SID trade tape sends the sale, the PAX tape nothing.
func TestTopicsSpotSaleWakesBaseTape(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	m, err := telemetry.New(config.Indexer)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := decode.NewRegistry(m, decode.Options{PerpsModule: decode.ModuleAddress("perps"), SpotModule: decode.ModuleAddress("spot"),
		PricefeedModule: decode.ModuleAddress("pricefeed")})
	if err != nil {
		t.Fatal(err)
	}
	usdl, pax, sid, seller := common.BytesToAddress(usdlToken), common.BytesToAddress(paxToken), common.BytesToAddress(sidToken),
		common.BytesToAddress(trader)
	write := func(n uint64, logs ...decode.Log) {
		t.Helper()
		rows, err := reg.Decode(ctx, nil, logs)
		if err != nil {
			t.Fatalf("decode block %d: %v", n, err)
		}
		if len(rows.Raw) != 0 {
			t.Fatalf("block %d raw rows %+v", n, rows.Raw)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := store.WriteBlock(ctx, tx, chainBlock(n), rows); err != nil {
			t.Fatalf("WriteBlock %d: %v", n, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	b1 := chainBlock(1)
	var registered []decode.Log
	for i, tok := range []struct {
		token  common.Address
		stable bool
	}{{usdl, true}, {pax, false}, {sid, false}} {
		registered = append(registered, spotLog(t, spotabi.Vault, "TokenRegistered", decode.SpotVaultAddress, 1, uint(i), decode.ReceiptActivation,
			nil, b1.Time, tok.token, uint8(6), tok.stable))
	}
	write(1, registered...)

	p := providers(t, db)
	for _, topic := range []string{"SID@trade", "PAX@trade"} {
		snap := decodeJSON[struct {
			Trades []map[string]any `json:"trades"`
		}](t, subscribe(t, p, topic))
		if len(snap.Trades) != 0 {
			t.Fatalf("%s snapshot before the sale %+v", topic, snap)
		}
	}

	b2 := chainBlock(2)
	write(2, spotLog(t, spotabi.PECOR, "SimpleSwap", decode.SpotPoolAddress, 2, 0, decode.ReceiptTx, &seller, b2.Time,
		seller, sid, usdl, big.NewInt(2_000_000), big.NewInt(3_000_000), big.NewInt(1_500_000_000_000_000_000)))

	rows, err := db.QueryContext(ctx, `SELECT subject, envelope FROM perps.delivery_outbox
WHERE convert_from(envelope, 'UTF8')::jsonb #>> '{source_order,height}' = '2' ORDER BY perps_seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var frames []Frame
	for rows.Next() {
		var (
			subject string
			wire    []byte
		)
		if err := rows.Scan(&subject, &wire); err != nil {
			t.Fatal(err)
		}
		env, err := delivery.Decode(wire, store.Policy(125), subject)
		if err != nil {
			t.Fatalf("outbox %s: %v", subject, err)
		}
		got, resnap := message(t, p, subject, env)
		if len(resnap) != 0 {
			t.Fatalf("%s asked for snapshots %v", subject, resnap)
		}
		frames = append(frames, got...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n := len(only(frames, "PAX@trade")); n != 0 {
		t.Fatalf("PAX tape sent %d frames for a SID sale", n)
	}
	sold := only(frames, "SID@trade")
	if len(sold) != 1 {
		t.Fatalf("SID trade frames %v, want one", frames)
	}
	got := decodeJSON[map[string]any](t, sold[0].Data)
	if got["symbol"] != "SID" || got["side"] != "SELL" || got["price"] != 1.5 || got["size"] != 2.0 {
		t.Fatalf("SID trade %v", got)
	}
}
