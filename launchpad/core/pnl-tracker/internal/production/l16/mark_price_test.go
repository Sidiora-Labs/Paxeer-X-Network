package l16

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestSpotMarkValuesWholeTokensInUSDL(t *testing.T) {
	token := "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	at := l17.ChainPosition{ChainID: "125", Height: "100", TransactionIndex: "0", LogIndex: "2", TransactionHash: "0x" + strings.Repeat("ab", 32), BlockHash: "0x" + strings.Repeat("cd", 32)}
	observed := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	// 10,250 USDL effective against 1,000,000 whole 18-decimal tokens:
	// 0.01025 USDL per token = 41/4000.
	effective := big.NewInt(10_250_000_000)
	reserve := new(big.Int).Mul(big.NewInt(1_000_000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	mark, err := SpotMark(token, 18, effective, reserve, "fill-1", at, observed, "0xce20da50f92d21d8bee497910c782e9aa25b6263")
	if err != nil {
		t.Fatal(err)
	}
	if mark.Numerator != "41" || mark.Denominator != "4000" {
		t.Fatalf("mark %s/%s", mark.Numerator, mark.Denominator)
	}
	watermark := l17.Watermark{ChainID: "125", Height: "100", BlockHash: at.BlockHash, ProjectionRevision: "1", ObservedAt: observed.Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	// 2,000 whole tokens bought for 15 USDL: worth 20.5 USDL at the mark.
	balance := new(big.Int).Mul(big.NewInt(2000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)).String()
	position, err := ProjectPosition("0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154", token, Inventory{Known: balance, Unknown: "0", Cost: "15000000", Realized: "0", UnknownProceeds: "0", Version: 2}, watermark, mark, "priced")
	if err != nil {
		t.Fatal(err)
	}
	if position.PricingQuality != "priced" || position.UnrealizedPnlRaw == nil || *position.UnrealizedPnlRaw != "5500000" {
		t.Fatalf("position %+v", position)
	}
	if _, err = SpotMark(token, 18, big.NewInt(0), reserve, "fill-1", at, observed, "0x"); err == nil {
		t.Fatal("zero reserve accepted")
	}
}

// TestMarkPriceAgainstPaxeerPool reads the real chain-125 pool
// 0xCE20...6263 at the block of its last swap and checks that the pinned
// effective reserves, the reserve-settlement scan and the resulting mark agree
// with the pool's own getPrice() at that block. NETWORK_RPC_URL must still
// retain the swap block (api5 keeps about 100k blocks; api2 is an archive).
func TestMarkPriceAgainstPaxeerPool(t *testing.T) {
	endpoint := os.Getenv("NETWORK_RPC_URL")
	if endpoint == "" {
		t.Skip("NETWORK_RPC_URL not set: live chain-125 read skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const swapBlock = 33963749
	_, reported, err := reportedHeaderByNumber(ctx, client, big.NewInt(swapBlock))
	if err != nil {
		t.Skipf("swap block %d not served by %s: %v", swapBlock, endpoint, err)
	}
	blockHash := common.HexToHash(reported)
	pool := common.HexToAddress("0xce20da50f92d21d8bee497910c782e9aa25b6263")
	token := "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	emitter := "0x622e1bb90e0e2b6e6e5e9e2dbe1555389e8a3ef6"
	a := &BaselineAuthority{rpc: client}
	tokenAddress := common.HexToAddress(token)
	decimalRaw, err := client.CallContractAtHash(ctx, ethereum.CallMsg{To: &tokenAddress, Data: decimalsSelector}, blockHash)
	if err != nil || len(decimalRaw) != 32 {
		t.Fatalf("decimals %x %v", decimalRaw, err)
	}
	decimals := int(new(big.Int).SetBytes(decimalRaw).Int64())
	fill := accounting.FillV1{ActualEmitter: emitter, PoolAddress: strings.ToLower(pool.Hex()), Token: accounting.Asset{Address: token, Decimals: uint8(decimals)}, Settlement: accounting.Asset{Address: usdlAddress, Decimals: 6}}
	settled, err := a.reservesSettledAfter(ctx, blockHash, pool, fill, 0, 2)
	if err != nil || !settled {
		t.Fatalf("last swap of the block: settled=%v err=%v", settled, err)
	}
	// The swap's own pool transfers precede its Swap log, so a cursor before
	// them must see reserve movement after it.
	if settled, err = a.reservesSettledAfter(ctx, blockHash, pool, fill, 0, 0); err == nil && settled {
		t.Fatal("reserve transfers after the cursor were not detected")
	}
	reserves, err := client.CallContractAtHash(ctx, ethereum.CallMsg{To: &pool, Data: effectiveReservesSelector}, blockHash)
	if err != nil || len(reserves) != 64 {
		t.Fatalf("effective reserves %x %v", reserves, err)
	}
	effective, tokenReserve := new(big.Int).SetBytes(reserves[:32]), new(big.Int).SetBytes(reserves[32:])
	priceRaw, err := client.CallContractAtHash(ctx, ethereum.CallMsg{To: &pool, Data: crypto.Keccak256([]byte("getPrice()"))[:4]}, blockHash)
	if err != nil || len(priceRaw) != 32 {
		t.Fatalf("getPrice %x %v", priceRaw, err)
	}
	wad := new(big.Int).Quo(new(big.Int).Mul(effective, big.NewInt(1e18)), tokenReserve)
	if wad.Cmp(new(big.Int).SetBytes(priceRaw)) != 0 {
		t.Fatalf("effective reserves price %s != pool getPrice %s", wad, new(big.Int).SetBytes(priceRaw))
	}
	mark, err := SpotMark(token, decimals, effective, tokenReserve, "fill", l17.ChainPosition{ChainID: "125", Height: "33963749", TransactionIndex: "0", LogIndex: "2", TransactionHash: "0xf5f9ed1136e71f03ffd44ae7f80ee192998b80fccd96ad3f75824fa9ef3c2845", BlockHash: reported}, time.Now().UTC(), pool.Hex())
	if err != nil {
		t.Fatal(err)
	}
	n, _ := new(big.Int).SetString(mark.Numerator, 10)
	d, _ := new(big.Int).SetString(mark.Denominator, 10)
	// getPrice is raw USDL per raw token in WAD; the mark is human USDL per
	// whole token, so WAD = mark * 1e18 * 10^6 / 10^decimals.
	scaled := new(big.Int).Mul(n, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(24-decimals)), nil))
	scaled.Quo(scaled, d)
	if scaled.Cmp(wad) != 0 && new(big.Int).Sub(wad, scaled).CmpAbs(big.NewInt(1)) > 0 {
		t.Fatalf("mark %s/%s scaled %s, pool price %s", mark.Numerator, mark.Denominator, scaled, wad)
	}
	t.Logf("pool %s at block %d: token decimals %d, effective USDL %s, token reserve %s, mark %s/%s USDL per token", pool.Hex(), swapBlock, decimals, effective, tokenReserve, mark.Numerator, mark.Denominator)
}
