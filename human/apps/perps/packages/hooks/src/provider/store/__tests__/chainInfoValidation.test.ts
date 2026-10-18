import {
  mainnetChainFallback,
  testnetChainFallback,
} from "../chainInfoFallback";
import { sanitizeChainInfoData } from "../chainInfoValidation";

const evmChain = {
  chain_id: "42161",
  name: "Arbitrum",
  public_rpc_url: "https://arb1.arbitrum.io/rpc",
  currency_symbol: "ETH",
  currency_decimal: 18,
  explorer_base_url: "https://arbiscan.io",
  vault_address: "0xvault",
};

const solanaChain = {
  chain_id: "900900900",
  name: "Solana",
  public_rpc_url: "https://api.mainnet-beta.solana.com",
  currency_symbol: "SOL",
  currency_decimal: 9,
  explorer_base_url: "https://explorer.solana.com",
  vault_address: "vault",
};

describe("sanitizeChainInfoData", () => {
  it("keeps valid rows and drops invalid ones", () => {
    const invalidRow = { ...evmChain, public_rpc_url: "" };
    const result = sanitizeChainInfoData([evmChain, invalidRow, solanaChain]);

    expect(result).toEqual([evmChain, solanaChain]);
    expect(result?.[0]).toBe(evmChain);
    expect(result?.[1]).toBe(solanaChain);
  });

  it("passes the configured mainnet and testnet static fallbacks through", () => {
    expect(sanitizeChainInfoData(mainnetChainFallback)).toEqual(
      mainnetChainFallback,
    );
    expect(sanitizeChainInfoData(testnetChainFallback)).toEqual(
      testnetChainFallback,
    );
  });

  it("normalizes a numeric-string currency_decimal", () => {
    const result = sanitizeChainInfoData([
      { ...evmChain, currency_decimal: "18" },
    ]);

    expect(result).toEqual([evmChain]);
  });

  it("keeps a number currency_decimal untouched", () => {
    const result = sanitizeChainInfoData([evmChain]);

    expect(result?.[0]).toBe(evmChain);
  });

  it("keeps a row with an empty explorer_base_url (display-only field)", () => {
    const noExplorerChain = { ...evmChain, explorer_base_url: "" };
    const result = sanitizeChainInfoData([noExplorerChain]);

    expect(result).toEqual([noExplorerChain]);
    expect(result?.[0]).toBe(noExplorerChain);
  });

  it("keeps a Solana-only chain list", () => {
    const result = sanitizeChainInfoData([solanaChain]);

    expect(result).toEqual([solanaChain]);
    expect(result?.[0]).toBe(solanaChain);
  });

  it.each([
    ["empty array", []],
    ["null", null],
    ["object", {}],
    ["null rows only", [null]],
    ["missing chain id", [{ ...evmChain, chain_id: undefined }]],
    ["missing RPC", [{ ...evmChain, public_rpc_url: "" }]],
    ["invalid currency decimals", [{ ...evmChain, currency_decimal: NaN }]],
    ["empty-string currency decimals", [{ ...evmChain, currency_decimal: "" }]],
    ["null currency decimals", [{ ...evmChain, currency_decimal: null }]],
    [
      "non-numeric currency decimals",
      [{ ...evmChain, currency_decimal: "abc" }],
    ],
    ["negative currency decimals", [{ ...evmChain, currency_decimal: -1 }]],
    ["boolean currency decimals", [{ ...evmChain, currency_decimal: true }]],
  ])("returns null for %s", (_name, data) => {
    expect(sanitizeChainInfoData(data)).toBe(null);
  });

  it("drops a row with invalid currency decimals but keeps valid siblings", () => {
    const otherEvmChain = { ...evmChain, chain_id: "56", name: "BSC" };
    const result = sanitizeChainInfoData([
      { ...evmChain, currency_decimal: "abc" },
      solanaChain,
      otherEvmChain,
    ]);

    expect(result).toEqual([solanaChain, otherEvmChain]);
  });
});
