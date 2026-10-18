import React from "react";
import { createRoot } from "react-dom/client";
import { act } from "react-dom/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ROBINHOOD_MAINNET_CHAINID,
  ROBINHOOD_TESTNET_CHAINID,
} from "@paxeer-network/types";
import { WithdrawTo } from "../../types";
import { useToken } from "../convertForm/hooks/useToken";
import { getQuoteTargetAmount } from "../convertForm/quoteAmount";
import { useWithdrawToken } from "../withdrawForm/hooks/useWithdrawToken";
import { canReverseConvert } from "./policy";

const data = vi.hoisted(() => ({ tokens: [] as any[], holdings: [] as any[] }));
vi.mock("@paxeer-network/hooks", () => ({
  useMainTokenStore: (select: any) => select({ data: data.tokens }),
  useTokensInfo: () => data.tokens,
  useHoldingStream: () => ({ data: data.holdings }),
  useConfig: () => "mainnet",
  useMemoizedFn: (fn: any) => fn,
}));
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
let root: ReturnType<typeof createRoot>;
let host: HTMLDivElement;
let result: any;
function mount(hook: () => any) {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  const Harness = () => {
    result = hook();
    return null;
  };
  act(() => root.render(<Harness />));
}
afterEach(() => {
  if (root) act(() => root.unmount());
  host?.remove();
});
const tokens = [
  { token: "USDC", decimals: 6, on_chain_swap: true },
  { token: "USDG", decimals: 6, on_chain_swap: false },
  { token: "USDT", decimals: 6, on_chain_swap: true },
];
describe("Robinhood withdrawal conversion", () => {
  it.each([
    ROBINHOOD_MAINNET_CHAINID,
    ROBINHOOD_TESTNET_CHAINID,
    String(ROBINHOOD_MAINNET_CHAINID),
  ])("allows reverse on %s", (id) =>
    expect(canReverseConvert(id, "USDC", "USDG")).toBe(true),
  );
  it("rejects another chain and missing target", () => {
    expect(canReverseConvert(1, "USDC", "USDG")).toBe(false);
    expect(canReverseConvert(ROBINHOOD_MAINNET_CHAINID, "USDC")).toBe(false);
  });
  it("uses a fixed pair and metadata decimals without holdings", () => {
    data.tokens = tokens;
    mount(() => useToken({ reverse: true }));
    expect(result.sourceToken.token).toBe("USDC");
    expect(result.targetToken.token).toBe("USDG");
    expect(result.targetToken.decimals).toBe(6);
    expect(result.sourceTokens).toEqual([]);
  });
  it("decodes the DEV CeFi quote sample with metadata decimals", () => {
    data.tokens = tokens;
    mount(() => useToken({ reverse: true }));
    // Real /v1/swap/quote response (USDC -> USDG, 2026-09 DEV): 1.2 USDC in,
    // toToken.estimatedAmount "1200240" in minimal units; CeFi omits
    // estimatedValue so decoding must use 10^token metadata decimals.
    expect(
      getQuoteTargetAmount(undefined, "1200240", result.targetToken.decimals),
    ).toBe("1.20024");
  });
  it("does not fabricate missing metadata", () => {
    data.tokens = tokens.filter((t) => t.token !== "USDG");
    mount(() => useToken({ reverse: true }));
    expect(result.targetToken).toBeUndefined();
  });
  it("keeps forward conversions", () => {
    data.tokens = tokens;
    mount(() => useToken({ defaultValue: "USDT" }));
    expect(result.sourceToken.token).toBe("USDT");
    expect(result.targetToken.token).toBe("USDC");
  });
  it("lists CeFi-swap tokens (on_chain_swap false) as forward sources", () => {
    data.tokens = tokens;
    mount(() => useToken({ defaultValue: "USDG" }));
    expect(result.sourceToken.token).toBe("USDG");
    expect(result.targetToken.token).toBe("USDC");
    expect(
      result.sourceTokens.map(({ token }: { token: string }) => token),
    ).toEqual(expect.arrayContaining(["USDT", "USDG"]));
  });
  it("selects USDG for external withdrawal even with zero USDG holdings", () => {
    data.tokens = tokens;
    data.holdings = [{ token: "USDC", holding: 1250 }];
    const currentChain = {
      id: ROBINHOOD_MAINNET_CHAINID,
      info: {
        token_infos: [
          { symbol: "USDG", token_decimal: 6, is_collateral: true },
        ],
      },
    } as any;
    mount(() =>
      useWithdrawToken({ withdrawTo: WithdrawTo.Wallet, currentChain }),
    );
    expect(result.sourceToken.symbol).toBe("USDG");
    expect(result.sourceToken.token_decimal).toBe(6);
    expect(result.sourceTokens.map((t: any) => t.symbol)).toEqual(["USDG"]);
    expect(result.isTokenSupportedOnChain("USDG")).toBe(true);
  });
  it("keeps internal account transfers unchanged", () => {
    data.tokens = tokens;
    data.holdings = [{ token: "USDC", holding: 1250 }];
    mount(() =>
      useWithdrawToken({
        withdrawTo: WithdrawTo.Account,
        currentChain: { id: ROBINHOOD_MAINNET_CHAINID } as any,
      }),
    );
    expect(result.sourceToken.symbol).toBe("USDC");
  });
});
