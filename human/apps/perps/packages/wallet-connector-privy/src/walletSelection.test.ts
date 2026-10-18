import { describe, expect, it } from "vitest";
import { AbstractChains, ChainNamespace } from "@paxeer-network/types";
import { WalletConnectType, WalletType } from "./types";
import {
  selectAggregatedWallet,
  shouldPreservePrivyEvmStorageChain,
} from "./walletSelection";

const createWallet = (
  address: string,
  namespace = ChainNamespace.evm,
  chainId = 1,
) => ({
  label: address,
  icon: "",
  provider: {} as any,
  accounts: [{ address }],
  chains: [{ id: chainId, namespace }],
});

const emptyOptions = {
  privyWalletEVM: null,
  privyWalletSOL: null,
  privyWalletEVMReady: true,
  privyWalletSOLReady: true,
  walletEVM: null,
  walletSOL: null,
  walletAbstract: null,
  isConnectedEVM: false,
  isConnectedSOL: false,
  isConnectedAbstract: false,
};

describe("selectAggregatedWallet", () => {
  it("uses the selected connector instead of the cached chain type", () => {
    const evm = createWallet("evm");
    const sol = createWallet("sol", ChainNamespace.solana, 900901);
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.EVM,
      storageChain: {
        chainId: 900901,
        namespace: ChainNamespace.solana,
      },
      walletEVM: evm,
      walletSOL: sol,
      isConnectedEVM: true,
      isConnectedSOL: true,
    });

    expect(result).toEqual({ wallet: evm, walletType: WalletConnectType.EVM });
  });

  it("stays disconnected when an Abstract chain is selected with the Privy connector", () => {
    const evm = createWallet("evm");
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      storageChain: {
        chainId: [...AbstractChains][0],
        namespace: ChainNamespace.evm,
      },
      privyWalletEVM: evm,
      privyWalletSOL: createWallet("sol", ChainNamespace.solana, 900901),
    });

    expect(result).toBeNull();
  });

  it("waits while the preferred Privy wallet type is still initializing", () => {
    const sol = createWallet("sol", ChainNamespace.solana, 900901);
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      storageChain: { chainId: 1, namespace: ChainNamespace.evm },
      privyWalletSOL: sol,
      privyWalletEVMReady: false,
    });

    expect(result).toBeNull();
  });

  it("falls back to a ready Solana wallet when the preferred EVM wallet is unavailable", () => {
    const sol = createWallet("sol", ChainNamespace.solana, 900901);
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      storageChain: { chainId: 1, namespace: ChainNamespace.evm },
      privyWalletSOL: sol,
    });

    expect(result).toEqual({
      wallet: sol,
      walletType: WalletConnectType.PRIVY,
    });
  });

  it("falls back to a ready EVM wallet when the preferred Solana wallet is unavailable", () => {
    const evm = createWallet("evm");
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      storageChain: {
        chainId: 900901,
        namespace: ChainNamespace.solana,
      },
      privyWalletEVM: evm,
    });

    expect(result).toEqual({
      wallet: evm,
      walletType: WalletConnectType.PRIVY,
    });
  });

  it("falls back to a ready Privy wallet without a selected chain", () => {
    const sol = createWallet("sol", ChainNamespace.solana, 900901);
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      storageChain: undefined,
      privyWalletSOL: sol,
    });

    expect(result).toEqual({
      wallet: sol,
      walletType: WalletConnectType.PRIVY,
    });
  });

  it("prefers the cached Privy namespace when both wallet types are ready", () => {
    const evm = createWallet("evm");
    const sol = createWallet("sol", ChainNamespace.solana, 900901);
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      storageChain: {
        chainId: 900901,
        namespace: ChainNamespace.solana,
      },
      privyWalletEVM: evm,
      privyWalletSOL: sol,
    });

    expect(result).toEqual({
      wallet: sol,
      walletType: WalletConnectType.PRIVY,
    });
  });

  it("uses the explicit Privy wallet target instead of the cached namespace", () => {
    const evm = createWallet("evm");
    const sol = createWallet("sol", ChainNamespace.solana, 900900900);
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      targetWalletType: WalletType.SOL,
      storageChain: { chainId: 1, namespace: ChainNamespace.evm },
      privyWalletEVM: evm,
      privyWalletSOL: sol,
    });

    expect(result).toEqual({
      wallet: sol,
      walletType: WalletConnectType.PRIVY,
    });
  });

  it("does not fall back while the explicit Privy wallet target is pending", () => {
    const evm = createWallet("evm");
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.PRIVY,
      targetWalletType: WalletType.SOL,
      storageChain: { chainId: 1, namespace: ChainNamespace.evm },
      privyWalletEVM: evm,
      privyWalletSOLReady: false,
    });

    expect(result).toBeNull();
  });

  it("returns the currently selected wallet object for the same connector type", () => {
    const selectedEvmWallet = createWallet("evm-2");
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.EVM,
      walletEVM: selectedEvmWallet,
      isConnectedEVM: true,
    });

    expect(result?.wallet).toBe(selectedEvmWallet);
  });

  it("selects Abstract for an explicit Abstract connection", () => {
    const abstract = createWallet(
      "abstract",
      ChainNamespace.evm,
      [...AbstractChains][0],
    );
    const result = selectAggregatedWallet({
      ...emptyOptions,
      connectorKey: WalletConnectType.ABSTRACT,
      walletAbstract: abstract,
      isConnectedAbstract: true,
    });

    expect(result).toEqual({
      wallet: abstract,
      walletType: WalletConnectType.ABSTRACT,
    });
  });
});

describe("shouldPreservePrivyEvmStorageChain", () => {
  it("preserves a supported Privy EVM target while the wallet is stale", () => {
    expect(
      shouldPreservePrivyEvmStorageChain({
        connectorKey: WalletConnectType.PRIVY,
        storageChain: { chainId: 421614, namespace: ChainNamespace.evm },
        nextChain: { chainId: 1, namespace: ChainNamespace.evm },
        supportedEvmChainIds: new Set([421614]),
      }),
    ).toBe(true);
  });

  it("allows normal synchronization outside a supported Privy EVM target", () => {
    expect(
      shouldPreservePrivyEvmStorageChain({
        connectorKey: WalletConnectType.EVM,
        storageChain: { chainId: 421614, namespace: ChainNamespace.evm },
        nextChain: { chainId: 1, namespace: ChainNamespace.evm },
        supportedEvmChainIds: new Set([421614]),
      }),
    ).toBe(false);
  });
});
