import { filterMarkets } from "../markets/marketFilter";
import { MarketsType } from "../markets/marketTypes";
import type { MarketsItem } from "../markets/useMarkets";

const baseMarket = (overrides: Partial<MarketsItem>): MarketsItem =>
  ({
    symbol: "PERP_BTC_USDC",
    broker_id: null,
    index_price: 1,
    mark_price: 1,
    sum_unitary_funding: 0,
    est_funding_rate: 0,
    last_funding_rate: 0,
    next_funding_time: 0,
    open_interest: 0,
    "24h_open": 1,
    "24h_close": 1,
    "24h_high": 1,
    "24h_low": 1,
    "24h_volume": 0,
    "24h_amount": 0,
    "24h_volumn": 0,
    change: 0,
    "8h_funding": 0,
    quote_dp: 2,
    created_time: 0,
    openInterest: 0,
    isFavorite: false,
    isRwa: false,
    isPreTge: false,
    ...overrides,
  }) as MarketsItem;

const filter = (type: MarketsType, markets: MarketsItem[]) =>
  filterMarkets({
    markets,
    favorites: [],
    recent: [],
    newListing: [],
    type,
  });

describe("filterMarkets", () => {
  const normal = baseMarket({ symbol: "PERP_BTC_USDC" });
  const preTge = baseMarket({
    symbol: "PERP_FRIEND_USDC",
    isPreTge: true,
  });
  const rwa = baseMarket({
    symbol: "PERP_AAPL_USDC",
    isRwa: true,
  });

  it("returns only Pre-TGE markets for PRE_TGE type", () => {
    expect(filter(MarketsType.PRE_TGE, [normal, preTge, rwa])).toEqual([
      preTge,
    ]);
  });

  it("keeps Pre-TGE markets in ALL", () => {
    expect(filter(MarketsType.ALL, [normal, preTge])).toEqual([normal, preTge]);
  });

  it("returns non-RWA markets for CRYPTO type", () => {
    expect(filter(MarketsType.CRYPTO, [normal, preTge, rwa])).toEqual([
      normal,
      preTge,
    ]);
  });

  it("does not include non-RWA Pre-TGE markets in RWA", () => {
    expect(filter(MarketsType.RWA, [preTge, rwa])).toEqual([rwa]);
  });
});
