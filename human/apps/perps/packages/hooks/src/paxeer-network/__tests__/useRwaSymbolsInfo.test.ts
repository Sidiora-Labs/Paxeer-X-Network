import {
  getExpiredRwaSymbolsInfoRefreshKeys,
  getRwaSymbolsInfoRefreshState,
} from "../rwaSymbolsInfoRevalidator";
import { isCurrentlyTrading } from "../useRwaSymbolsInfo";

const createRwaSymbol = (
  symbol: string,
  overrides: {
    status: "open" | "close";
    next_open: number;
    next_close: number;
  },
) => {
  const [, base = "", quote = ""] = symbol.split("_");

  return {
    symbol,
    market_session: "US_STOCK",
    base,
    quote,
    type: "PERP",
    name: `${base}-PERP`,
    ...overrides,
  };
};

describe("isCurrentlyTrading", () => {
  const currentTime = 1_000_000;

  it("switches a closed RWA symbol to open after next_open passes", () => {
    expect(
      isCurrentlyTrading(
        currentTime + 60_000,
        "close",
        currentTime,
        currentTime - 1_000,
      ),
    ).toBe(true);
  });

  it("keeps a closed RWA symbol closed before next_open", () => {
    expect(
      isCurrentlyTrading(
        currentTime + 60_000,
        "close",
        currentTime,
        currentTime + 1_000,
      ),
    ).toBe(false);
  });

  it("switches an open RWA symbol to closed after next_close passes", () => {
    expect(isCurrentlyTrading(currentTime - 1_000, "open", currentTime)).toBe(
      false,
    );
  });
});

describe("RWA symbols info refresh state", () => {
  const currentTime = 1_000_000;

  it("does not request refresh for a closed symbol before next_close", () => {
    const symbol = "PERP_KRQA_USDC";
    const refreshState = getRwaSymbolsInfoRefreshState(
      {
        [symbol]: createRwaSymbol(symbol, {
          status: "close",
          next_open: currentTime - 1_000,
          next_close: currentTime + 1_000,
        }),
      },
      currentTime,
      {},
    );

    expect(refreshState.shouldRefresh).toBe(false);
  });

  it("requests refresh for a closed symbol after next_close", () => {
    const symbol = "PERP_KRQA_USDC";
    const refreshState = getRwaSymbolsInfoRefreshState(
      {
        [symbol]: createRwaSymbol(symbol, {
          status: "close",
          next_open: currentTime - 2_000,
          next_close: currentTime - 1_000,
        }),
      },
      currentTime,
      {},
    );

    expect(refreshState.shouldRefresh).toBe(true);
  });

  it("requests refresh for an open symbol after next_close", () => {
    const symbol = "PERP_US500_USDC";
    const refreshState = getRwaSymbolsInfoRefreshState(
      {
        [symbol]: createRwaSymbol(symbol, {
          status: "open",
          next_open: currentTime + 60_000,
          next_close: currentTime - 1_000,
        }),
      },
      currentTime,
      {},
    );

    expect(refreshState.shouldRefresh).toBe(true);
  });

  it("does not request refresh twice for the same stale snapshot", () => {
    const symbol = "PERP_US500_USDC";
    const rwaSymbolsInfo = {
      [symbol]: createRwaSymbol(symbol, {
        status: "open" as const,
        next_open: currentTime + 60_000,
        next_close: currentTime - 1_000,
      }),
    };
    const firstState = getRwaSymbolsInfoRefreshState(
      rwaSymbolsInfo,
      currentTime,
      {},
    );
    const secondState = getRwaSymbolsInfoRefreshState(
      rwaSymbolsInfo,
      currentTime + 1_000,
      firstState.triggeredRefreshKeys,
    );

    expect(firstState.shouldRefresh).toBe(true);
    expect(secondState.shouldRefresh).toBe(false);
  });

  it("groups multiple stale symbols into one refresh decision", () => {
    const firstSymbol = "PERP_US500_USDC";
    const secondSymbol = "PERP_US100_USDC";
    const refreshKeys = getExpiredRwaSymbolsInfoRefreshKeys(
      {
        [firstSymbol]: createRwaSymbol(firstSymbol, {
          status: "open",
          next_open: currentTime + 60_000,
          next_close: currentTime - 1_000,
        }),
        [secondSymbol]: createRwaSymbol(secondSymbol, {
          status: "open",
          next_open: currentTime + 60_000,
          next_close: currentTime - 1_000,
        }),
      },
      currentTime,
      {},
    );

    expect(refreshKeys).toHaveLength(2);
    expect(refreshKeys[0]).toContain(firstSymbol);
    expect(refreshKeys[1]).toContain(secondSymbol);
  });

  it("requests refresh again when the stale snapshot key changes", () => {
    const symbol = "PERP_US500_USDC";
    const firstState = getRwaSymbolsInfoRefreshState(
      {
        [symbol]: createRwaSymbol(symbol, {
          status: "open",
          next_open: currentTime + 60_000,
          next_close: currentTime - 1_000,
        }),
      },
      currentTime,
      {},
    );
    const secondState = getRwaSymbolsInfoRefreshState(
      {
        [symbol]: createRwaSymbol(symbol, {
          status: "open",
          next_open: currentTime + 120_000,
          next_close: currentTime - 1_000,
        }),
      },
      currentTime + 1_000,
      firstState.triggeredRefreshKeys,
    );

    expect(firstState.shouldRefresh).toBe(true);
    expect(secondState.shouldRefresh).toBe(true);
  });
});
