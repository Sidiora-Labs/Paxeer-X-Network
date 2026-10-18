package chainflowdatagateway

import (
	"testing"
)

func TestCoinGeckoListMarketsParamsCommaJoinsIDs(t *testing.T) {
	query := CoinGeckoListMarketsParams{
		IDs: F([]CoinID{CoinIDBitcoin, CoinIDEthereum}),
	}.URLQuery()
	if got := query["ids"]; len(got) != 1 || got[0] != "bitcoin,ethereum" {
		t.Fatalf("ids = %q, want one value \"bitcoin,ethereum\"", got)
	}
	if got, want := query.Encode(), "ids=bitcoin%2Cethereum"; got != want {
		t.Fatalf("encoded query = %q, want %q", got, want)
	}
}

func TestCoinGeckoListMarketsParamsCommaJoinsSymbolsAndPriceChangePercentage(t *testing.T) {
	query := CoinGeckoListMarketsParams{
		Symbols:               F([]Market{MarketBtc, MarketEth}),
		PriceChangePercentage: F([]CoinGeckoListMarketsParamsPriceChangePercentage{CoinGeckoListMarketsParamsPriceChangePercentage1h, CoinGeckoListMarketsParamsPriceChangePercentage24h}),
	}.URLQuery()
	if got := query["symbols"]; len(got) != 1 || got[0] != "btc,eth" {
		t.Fatalf("symbols = %q, want one value \"btc,eth\"", got)
	}
	if got := query["price_change_percentage"]; len(got) != 1 || got[0] != "1h,24h" {
		t.Fatalf("price_change_percentage = %q, want one value \"1h,24h\"", got)
	}
}

func TestCoinGeckoListSimplePricesParamsCommaJoinsIDs(t *testing.T) {
	query := CoinGeckoListSimplePricesParams{
		IDs: F([]CoinID{CoinIDBitcoin, CoinIDEthereum}),
	}.URLQuery()
	if got := query["ids"]; len(got) != 1 || got[0] != "bitcoin,ethereum" {
		t.Fatalf("ids = %q, want one value \"bitcoin,ethereum\"", got)
	}
}
