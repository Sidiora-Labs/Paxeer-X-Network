package decode_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gorilla/websocket"

	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
)

const (
	// seedSymbol is the spot market the seed rests orders on, as the feed and the
	// share pointer both spell it.
	seedSymbol = "PAX"
	// seedNotionalUSD sizes every seeded order well above the $10 minimum notional.
	seedNotionalUSD = 25
)

var spotVaultAddress = common.HexToAddress("0x0000000000000000000000000000000000001025")
var spotQuoterAddress = common.HexToAddress("0x0000000000000000000000000000000000001024")

// chainflowBook is the data of one <SYMBOL>@orderbook frame: asks ascending and
// bids descending, each level [price, size].
type chainflowBook struct {
	Symbol string          `json:"symbol"`
	Asks   [][]json.Number `json:"asks"`
	Bids   [][]json.Number `json:"bids"`
}

// seedSpotRestingOrders reads the Chainflow order book of the PAX spot market at
// FEED_ORIGIN and rests orders on the node of PERPS_DATA_TEST_RPC through the
// real exchange calls, signed by the funded account whose hex key is in the file
// PERPS_DATA_TEST_SEED_KEY_FILE: a limit sell at the best ask on 0x1022, and a
// stop-limit sell at the best two bids and a stop-loss at the third bid on 0x1023.
func seedSpotRestingOrders(t *testing.T) {
	t.Helper()
	origin := requireEnv(t, "FEED_ORIGIN", "the spot seed reads the Chainflow order book of the feed origin")
	keyFile := requireEnv(t, "PERPS_DATA_TEST_SEED_KEY_FILE", "the spot seed signs its orders with a funded test account")
	rpcURL := requireEnv(t, "PERPS_DATA_TEST_RPC", "the spot seed places its orders on a node")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	book := readChainflowBook(ctx, t, origin, seedSymbol)
	if len(book.Asks) < 1 || len(book.Bids) < 3 {
		t.Fatalf("%s@orderbook holds %d asks and %d bids, the seed needs one ask and three bids", seedSymbol, len(book.Asks), len(book.Bids))
	}
	ask, bid0, bid1, bid2 := levelPrice(t, book.Asks[0]), levelPrice(t, book.Bids[0]), levelPrice(t, book.Bids[1]), levelPrice(t, book.Bids[2])
	if bid0.Cmp(ask) >= 0 || bid1.Cmp(bid0) > 0 || bid2.Cmp(bid1) > 0 {
		t.Fatalf("%s@orderbook is crossed or unordered: ask %s bids %s %s %s", seedSymbol, ask, bid0, bid1, bid2)
	}

	raw, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(string(raw)), "0x"))
	if err != nil {
		t.Fatalf("PERPS_DATA_TEST_SEED_KEY_FILE: %v", err)
	}
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	vault, quoter := loadABI(t, spotabi.Vault), loadABI(t, spotabi.Quoter)
	orders, stops := loadABI(t, spotabi.Orders), loadABI(t, spotabi.StopOrders)
	token := seedToken(ctx, t, client, vault, seedSymbol)
	stable := firstAddress(t, viewCall(ctx, t, client, vault, spotVaultAddress, "getRegisteredStablecoins"))
	decimals := viewCall(ctx, t, client, vault, spotVaultAddress, "getTokenDecimals", token)[0].(uint8)
	index := freshIndexPrice(ctx, t, client, quoter, token)

	// Size from the lowest seeded price so every order clears the minimum notional
	// at the index price the node applies.
	low := bid2
	if index.Cmp(low) < 0 {
		low = index
	}
	amount := new(big.Int).Mul(big.NewInt(seedNotionalUSD), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)+18), nil))
	amount.Add(amount, new(big.Int).Sub(low, big.NewInt(1)))
	amount.Quo(amount, low)
	expires := new(big.Int).SetInt64(time.Now().Add(24 * time.Hour).Unix())

	from := crypto.PubkeyToAddress(key.PublicKey)
	sendOrder(ctx, t, client, key, from, orders, decode.SpotOrdersAddress, "LimitOrderCreated",
		"placeLimitSell", token, stable, amount, ask, expires)
	sendOrder(ctx, t, client, key, from, stops, decode.SpotStopAddress, "StopLimitOrderCreated",
		"placeStopLimitSell", token, stable, amount, bid0, bid1, expires)
	sendOrder(ctx, t, client, key, from, stops, decode.SpotStopAddress, "StopLossOrderCreated",
		"placeStopLoss", token, stable, amount, bid2, expires)
}

func requireEnv(t *testing.T, name, why string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is required: %s", name, why)
	}
	return v
}

func loadABI(t *testing.T, load func() (abi.ABI, error)) abi.ABI {
	t.Helper()
	a, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// readChainflowBook subscribes to <SYMBOL>@orderbook on the /ws/<symbol> socket of
// the feed origin and returns the first book frame.
func readChainflowBook(ctx context.Context, t *testing.T, origin, symbol string) chainflowBook {
	t.Helper()
	u, err := url.Parse(origin)
	if err != nil {
		t.Fatalf("FEED_ORIGIN %q: %v", origin, err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		t.Fatalf("FEED_ORIGIN %q must be an http or https origin", origin)
	}
	u.Path = "/ws/" + strings.ToLower(symbol)
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, resp, err := websocket.DefaultDialer.DialContext(dialCtx, u.String(), nil)
	if err != nil {
		status := ""
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("feed %s: %v %s", u.Redacted(), err, status)
	}
	defer conn.Close()
	topic := symbol + "@orderbook"
	if err := conn.WriteJSON(map[string]string{"event": "subscribe", "topic": topic}); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		var frame struct {
			Event   string          `json:"event"`
			Message string          `json:"message"`
			Topic   string          `json:"topic"`
			Data    json.RawMessage `json:"data"`
		}
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("feed %s %s: %v", u.Redacted(), topic, err)
		}
		if frame.Event == "error" {
			t.Fatalf("feed %s %s: error %s", u.Redacted(), topic, frame.Message)
		}
		if frame.Event != "" || frame.Topic != topic {
			continue
		}
		var book chainflowBook
		if err := json.Unmarshal(frame.Data, &book); err != nil {
			t.Fatalf("feed %s frame: %v", topic, err)
		}
		if book.Symbol != symbol {
			t.Fatalf("feed %s frame names symbol %q", topic, book.Symbol)
		}
		return book
	}
}

// levelPrice converts the decimal price of a [price, size] level to the 1e18 USD
// scale of the spot rails.
func levelPrice(t *testing.T, level []json.Number) *big.Int {
	t.Helper()
	if len(level) != 2 {
		t.Fatalf("book level %v is not [price, size]", level)
	}
	r, ok := new(big.Rat).SetString(level[0].String())
	if !ok || r.Sign() <= 0 {
		t.Fatalf("book level price %q", level[0])
	}
	r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
	return new(big.Int).Quo(r.Num(), r.Denom())
}

func viewCall(ctx context.Context, t *testing.T, client *ethclient.Client, a abi.ABI, to common.Address, method string, args ...any) []any {
	t.Helper()
	data, err := a.Pack(method, args...)
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		t.Fatalf("%s on %s: %v", method, to.Hex(), err)
	}
	values, err := a.Unpack(method, out)
	if err != nil {
		t.Fatalf("%s on %s: %v", method, to.Hex(), err)
	}
	return values
}

func firstAddress(t *testing.T, values []any) common.Address {
	t.Helper()
	list := values[0].([]common.Address)
	if len(list) == 0 {
		t.Fatal("the spot vault registers no stablecoin")
	}
	return list[0]
}

// seedToken returns the registered non-stable spot token whose ERC20 symbol is symbol.
func seedToken(ctx context.Context, t *testing.T, client *ethclient.Client, vault abi.ABI, symbol string) common.Address {
	t.Helper()
	erc20, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"symbol","stateMutability":"view","inputs":[],"outputs":[{"type":"string","name":""}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range viewCall(ctx, t, client, vault, spotVaultAddress, "getRegisteredTokens")[0].([]common.Address) {
		if viewCall(ctx, t, client, vault, spotVaultAddress, "isStablecoin", token)[0].(bool) {
			continue
		}
		if viewCall(ctx, t, client, erc20, token, "symbol")[0].(string) == symbol {
			return token
		}
	}
	t.Fatalf("the spot vault registers no %s token", symbol)
	return common.Address{}
}

// freshIndexPrice waits until the quoter answers a fresh index price for token.
func freshIndexPrice(ctx context.Context, t *testing.T, client *ethclient.Client, quoter abi.ABI, token common.Address) *big.Int {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		values := viewCall(ctx, t, client, quoter, spotQuoterAddress, "getTokenPrice", token)
		price, stale := values[0].(*big.Int), values[2].(bool)
		if !stale && price.Sign() > 0 {
			return price
		}
		if time.Now().After(deadline) {
			t.Fatalf("getTokenPrice(%s) stayed stale (price %s): the pricefeed of the node receives no fresh submissions", token.Hex(), price)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// sendOrder signs one call of method on rail, waits for its receipt and requires
// status 1 and the rail's created event in its logs.
func sendOrder(ctx context.Context, t *testing.T, client *ethclient.Client, key *ecdsa.PrivateKey, from common.Address, a abi.ABI, rail common.Address, created, method string, args ...any) {
	t.Helper()
	data, err := a.Pack(method, args...)
	if err != nil {
		t.Fatal(err)
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := client.NonceAt(ctx, from, nil)
	if err != nil {
		t.Fatal(err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gas, err := client.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &rail, GasPrice: gasPrice, Data: data})
	if err != nil {
		t.Fatalf("%s on %s: %v", method, rail.Hex(), err)
	}
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: nonce, GasPrice: gasPrice, Gas: gas * 2, To: &rail, Data: data}),
		ethtypes.LatestSignerForChainID(chainID), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendTransaction(ctx, tx); err != nil {
		t.Fatalf("%s on %s: %v", method, rail.Hex(), err)
	}
	for {
		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			if receipt.Status != ethtypes.ReceiptStatusSuccessful {
				t.Fatalf("%s on %s: tx %s status %d", method, rail.Hex(), tx.Hash().Hex(), receipt.Status)
			}
			for _, l := range receipt.Logs {
				if l.Address == rail && len(l.Topics) > 0 && l.Topics[0] == a.Events[created].ID {
					return
				}
			}
			t.Fatalf("%s on %s: tx %s emitted no %s", method, rail.Hex(), tx.Hash().Hex(), created)
		}
		if ctx.Err() != nil {
			t.Fatalf("%s on %s: no receipt for %s: %v", method, rail.Hex(), tx.Hash().Hex(), err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
