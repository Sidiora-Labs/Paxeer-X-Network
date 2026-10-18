import {
  normalizeSwapQuoteResponse,
  isSwapQuoteData,
  SWAP_QUOTE_URL,
  unwrapSwapQuoteResponse,
  type SwapQuoteData,
  type SwapQuoteRequest,
} from "../swapQuote";

const request: SwapQuoteRequest = {
  fromToken: "USDT",
  toToken: "USDC",
  slippage: 0.005,
  amount: 0.1,
};

const quote: SwapQuoteData = {
  pathId: "oq_test",
  traceId: "trace_test",
  chainId: "84532",
  fromToken: {
    tokenAddress: "0xfrom",
    amount: "100000",
    value: "0.09993",
  },
  toToken: {
    tokenAddress: "0xto",
    estimatedAmount: "100621",
    estimatedValue: "0.100621",
  },
  valueCurrency: "USD",
  netOutValue: "0.100621",
  priceImpactPercent: null,
  slippageLimitPercent: "0.5",
  gasEstimate: {
    gasUnits: "650000",
    gasPriceWei: null,
    nativeTokenSymbol: "ETH",
    estimatedFeeAmount: null,
    estimatedFeeValue: null,
  },
  expiresAt: 1785479395132,
};

describe("swap quote response normalization", () => {
  it("uses the provider-neutral quote endpoint", () => {
    expect(SWAP_QUOTE_URL).toBe("/v1/swap/quote");
  });

  it("uses token symbols without chain or address fields", () => {
    expect(request).toEqual({
      fromToken: "USDT",
      toToken: "USDC",
      slippage: 0.005,
      amount: 0.1,
    });
    expect(request).not.toHaveProperty("chainId");
    expect(request).not.toHaveProperty("fromTokenAddress");
    expect(request).not.toHaveProperty("toTokenAddress");
  });

  it("unwraps the paxeer-network success envelope", () => {
    expect(unwrapSwapQuoteResponse({ success: true, data: quote })).toEqual(
      quote,
    );
  });

  it("accepts netOutValue when estimatedValue is omitted", () => {
    const toToken = { ...quote.toToken };
    delete toToken.estimatedValue;

    expect(isSwapQuoteData({ ...quote, toToken })).toBe(true);
  });

  it("preserves stable error fields from the failure envelope", () => {
    expect(() =>
      normalizeSwapQuoteResponse({
        success: false,
        code: -1042,
        message: "Quote unavailable",
        timestamp: 1785479395132,
      }),
    ).toThrow("Quote unavailable");

    try {
      normalizeSwapQuoteResponse({
        success: false,
        code: -1042,
        message: "Quote unavailable",
        timestamp: 1785479395132,
      });
    } catch (error) {
      expect(error).toMatchObject({
        code: -1042,
        timestamp: 1785479395132,
      });
    }
  });

  it("rejects incomplete quote data", () => {
    expect(isSwapQuoteData({ ...quote, netOutValue: undefined })).toBe(false);
    expect(() => normalizeSwapQuoteResponse({ success: true })).toThrow(
      "Invalid quote response",
    );
  });
});
