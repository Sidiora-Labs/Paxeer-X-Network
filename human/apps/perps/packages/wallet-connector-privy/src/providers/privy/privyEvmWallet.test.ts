import { describe, expect, it, vi } from "vitest";
import { ChainNamespace } from "@paxeer-network/types";
import { buildPrivyEvmWallet, parsePrivyEvmChainId } from "./privyEvmWallet";

describe("Privy EVM wallet", () => {
  it("parses CAIP-2 and numeric chain IDs without accepting invalid values", () => {
    expect(parsePrivyEvmChainId("eip155:421614")).toBe(421614);
    expect(parsePrivyEvmChainId("421614")).toBe(421614);
    expect(parsePrivyEvmChainId(421614)).toBe(421614);
    expect(parsePrivyEvmChainId("eip155:unknown")).toBeNull();
    expect(parsePrivyEvmChainId(undefined)).toBeNull();
  });

  it("switches to the selected chain before exposing the wallet", async () => {
    const provider = {};
    const wallet = {
      address: "0x1",
      chainId: "eip155:1",
      switchChain: vi.fn().mockResolvedValue(undefined),
      getEthereumProvider: vi.fn().mockResolvedValue(provider),
    };

    const result = await buildPrivyEvmWallet(wallet, 421614);

    expect(wallet.switchChain).toHaveBeenCalledWith(421614);
    expect(wallet.getEthereumProvider).toHaveBeenCalledAfter(
      wallet.switchChain,
    );
    expect(result).toMatchObject({
      provider,
      accounts: [{ address: "0x1" }],
      chains: [{ id: 421614, namespace: ChainNamespace.evm }],
      chain: { id: 421614, namespace: ChainNamespace.evm },
    });
  });

  it("does not switch when the wallet is already on the selected chain", async () => {
    const wallet = {
      address: "0x1",
      chainId: "eip155:421614",
      switchChain: vi.fn(),
      getEthereumProvider: vi.fn().mockResolvedValue({}),
    };

    await buildPrivyEvmWallet(wallet, 421614);

    expect(wallet.switchChain).not.toHaveBeenCalled();
  });

  it("keeps the real chain when automatic switching fails", async () => {
    const wallet = {
      address: "0x1",
      chainId: "eip155:1",
      switchChain: vi.fn().mockRejectedValue(new Error("switch failed")),
      getEthereumProvider: vi.fn().mockResolvedValue({}),
    };

    const result = await buildPrivyEvmWallet(wallet, 421614);

    expect(result.chains[0].id).toBe(1);
  });
});
