// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ChainNamespace,
  ConnectorKey,
  WALLET_CHAIN_CHANGE_PENDING_RESULT,
} from "@paxeer-network/types";
import { WalletConnectType, WalletType } from "../types";
import { useWallet } from "./useWallet";

const mocks = vi.hoisted(() => {
  const createWallet = (
    address: string,
    chainId: number,
    namespace: ChainNamespace,
  ) => ({
    label: address,
    icon: "",
    provider: {},
    accounts: [{ address }],
    chain: { id: chainId, namespace },
    chains: [{ id: chainId, namespace }],
  });

  return {
    connectorKey: "EVM",
    network: "testnet",
    setConnectorKey: vi.fn(),
    setStorageChain: vi.fn(),
    setOpenConnectDrawer: vi.fn(),
    setTargetWalletType: vi.fn(),
    track: vi.fn(),
    walletChainTypeConfig: {
      hasEvm: true,
      hasSol: true,
      hasAbstract: true,
    },
    evmConnected: true,
    solConnected: true,
    abstractConnected: true,
    privyConnected: false,
    privyEvm: null as ReturnType<typeof createWallet> | null,
    privySol: null as ReturnType<typeof createWallet> | null,
    privyEvmReady: true,
    privySolReady: true,
    evm: createWallet("0xevm", 421614, "EVM" as ChainNamespace),
    sol: createWallet("sol", 901901901, "SOL" as ChainNamespace),
    abstract: createWallet("0xabstract", 11124, "EVM" as ChainNamespace),
  };
});

vi.mock("@paxeer-network/hooks", () => ({
  useEventEmitter: () => ({ emit: vi.fn() }),
  useLocalStorage: (key: string) =>
    key === ConnectorKey
      ? [mocks.connectorKey, mocks.setConnectorKey]
      : [null, vi.fn()],
  useStorageChain: () => ({
    storageChain: { chainId: 421614, namespace: ChainNamespace.evm },
    setStorageChain: mocks.setStorageChain,
  }),
  useTrack: () => ({ track: mocks.track }),
}));

vi.mock("../provider", () => ({
  useWalletConnectorPrivy: () => ({
    initChains: [{ id: 421614 }, { id: 901901901 }, { id: 11124 }],
    mainnetChains: [{ id: 1 }, { id: 900900900 }],
    testnetChains: [{ id: 421614 }, { id: 901901901 }, { id: 11124 }],
    network: mocks.network,
    setOpenConnectDrawer: mocks.setOpenConnectDrawer,
    targetWalletType: undefined,
    setTargetWalletType: mocks.setTargetWalletType,
    walletChainTypeConfig: mocks.walletChainTypeConfig,
  }),
}));

vi.mock("../providers/wagmi/wagmiWalletProvider", () => ({
  useWagmiWallet: () => ({
    connect: vi.fn(),
    disconnect: vi.fn(),
    isConnected: mocks.evmConnected,
    setChain: vi.fn(),
    wallet: mocks.evm,
  }),
}));

vi.mock("../providers/solana/solanaWalletProvider", () => ({
  useSolanaWallet: () => ({
    connect: vi.fn(),
    disconnect: vi.fn(),
    isConnected: mocks.solConnected,
    wallet: mocks.sol,
  }),
}));

vi.mock("../providers/privy/privyWalletProvider", () => ({
  usePrivyWallet: () => ({
    connect: vi.fn(),
    disconnect: vi.fn(),
    switchChain: vi.fn(),
    walletEVM: mocks.privyEvm,
    walletSOL: mocks.privySol,
    walletEVMReady: mocks.privyEvmReady,
    walletSOLReady: mocks.privySolReady,
    isConnected: mocks.privyConnected,
  }),
}));

vi.mock("../providers/abstractWallet/abstractWalletProvider", () => ({
  useAbstractWallet: () => ({
    connect: vi.fn(),
    disconnect: vi.fn(),
    isConnected: mocks.abstractConnected,
    wallet: mocks.abstract,
  }),
}));

describe("useWallet switchWallet", () => {
  beforeEach(() => {
    mocks.connectorKey = "EVM";
    mocks.network = "testnet";
    mocks.evmConnected = true;
    mocks.solConnected = true;
    mocks.abstractConnected = true;
    mocks.privyConnected = false;
    mocks.privyEvm = null;
    mocks.privySol = null;
    mocks.privyEvmReady = true;
    mocks.privySolReady = true;
    mocks.walletChainTypeConfig.hasEvm = true;
    mocks.walletChainTypeConfig.hasSol = true;
    mocks.walletChainTypeConfig.hasAbstract = true;
    mocks.setConnectorKey.mockClear();
    mocks.setConnectorKey.mockImplementation((value: string) => {
      mocks.connectorKey = value;
    });
    mocks.setStorageChain.mockClear();
    mocks.setOpenConnectDrawer.mockClear();
    mocks.setTargetWalletType.mockClear();
    mocks.track.mockClear();
  });

  it.each([
    [WalletType.EVM, WalletConnectType.EVM, 421614],
    [WalletType.SOL, WalletConnectType.SOL, 901901901],
    [WalletType.ABSTRACT, WalletConnectType.ABSTRACT, 11124],
  ])(
    "keeps the connector and chain aligned when switching to %s",
    (walletType, connectorType, chainId) => {
      const { result } = renderHook(() => useWallet());

      act(() => result.current.switchWallet(walletType));

      expect(mocks.setConnectorKey).toHaveBeenCalledWith(connectorType);
      expect(mocks.setStorageChain).toHaveBeenCalledWith(chainId);
    },
  );

  it("marks a Privy Solana fallback as pending without reporting success", async () => {
    mocks.connectorKey = WalletConnectType.PRIVY;
    const { result } = renderHook(() => useWallet());

    await expect(result.current.setChain({ chainId: 901901901 })).resolves.toBe(
      WALLET_CHAIN_CHANGE_PENDING_RESULT,
    );

    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledWith(true);
    expect(mocks.setTargetWalletType).toHaveBeenCalledWith(WalletType.SOL);
  });

  it("marks a Privy Abstract fallback as pending without reporting success", async () => {
    mocks.connectorKey = WalletConnectType.PRIVY;
    const { result } = renderHook(() => useWallet());

    await expect(result.current.setChain({ chainId: 11124 })).resolves.toBe(
      WALLET_CHAIN_CHANGE_PENDING_RESULT,
    );

    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledWith(true);
    expect(mocks.setTargetWalletType).toHaveBeenCalledWith(WalletType.ABSTRACT);
  });

  it("keeps the chain switch pending while connecting an Abstract fallback wallet", async () => {
    mocks.abstractConnected = false;
    const { result } = renderHook(() => useWallet());

    await expect(result.current.setChain({ chainId: 11124 })).resolves.toBe(
      WALLET_CHAIN_CHANGE_PENDING_RESULT,
    );

    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledWith(true);
    expect(mocks.setTargetWalletType).toHaveBeenCalledWith(WalletType.ABSTRACT);
  });

  it("marks a disconnected EVM fallback as pending without reporting success", async () => {
    mocks.evmConnected = false;
    const { result } = renderHook(() => useWallet());

    await expect(result.current.setChain({ chainId: 421614 })).resolves.toBe(
      WALLET_CHAIN_CHANGE_PENDING_RESULT,
    );

    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledWith(true);
    expect(mocks.setTargetWalletType).toHaveBeenCalledWith(WalletType.EVM);
  });

  it("marks a disconnected Solana fallback as pending without reporting success", async () => {
    mocks.solConnected = false;
    const { result } = renderHook(() => useWallet());

    await expect(result.current.setChain({ chainId: 901901901 })).resolves.toBe(
      WALLET_CHAIN_CHANGE_PENDING_RESULT,
    );

    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledWith(true);
    expect(mocks.setTargetWalletType).toHaveBeenCalledWith(WalletType.SOL);
  });

  it("rejects a Privy chain switch when the target wallet type is unsupported", async () => {
    mocks.connectorKey = WalletConnectType.PRIVY;
    mocks.walletChainTypeConfig.hasSol = false;
    const { result } = renderHook(() => useWallet());

    await expect(
      result.current.setChain({ chainId: 901901901 }),
    ).rejects.toThrow("No solana wallet found");

    expect(mocks.setOpenConnectDrawer).not.toHaveBeenCalled();
    expect(mocks.setTargetWalletType).not.toHaveBeenCalled();
  });

  it.each([
    [WalletType.EVM, WalletConnectType.EVM, 421614],
    [WalletType.SOL, WalletConnectType.SOL, 901901901],
    [WalletType.ABSTRACT, WalletConnectType.ABSTRACT, 11124],
  ])(
    "keeps the connector and chain aligned when setChain selects %s",
    async (_walletType, connectorType, chainId) => {
      const { result } = renderHook(() => useWallet());

      await act(async () => {
        await result.current.setChain({ chainId });
      });

      expect(mocks.setConnectorKey).toHaveBeenCalledWith(connectorType);
      expect(mocks.setStorageChain).toHaveBeenCalledWith(chainId);
    },
  );

  it("restores the connector snapshot after a cancelled connection", () => {
    const { result } = renderHook(() => useWallet());

    act(() => result.current.restoreConnectorState("EVM", 421614));

    expect(mocks.setConnectorKey).toHaveBeenCalledWith("EVM");
    expect(mocks.setStorageChain).toHaveBeenCalledWith(421614);
  });

  it("uses only the active network when selecting a Privy EVM chain", () => {
    mocks.connectorKey = WalletConnectType.PRIVY;
    mocks.network = "mainnet";
    const { result } = renderHook(() => useWallet());

    act(() => result.current.switchWallet(WalletType.EVM, mocks.evm));

    expect(mocks.setStorageChain).toHaveBeenCalledWith(1);
    expect(mocks.setStorageChain).not.toHaveBeenCalledWith(421614);
  });

  it("activates another connected wallet after disconnecting the current one", async () => {
    const { result } = renderHook(() => useWallet());

    await act(async () => {
      await result.current.disconnect(WalletConnectType.EVM);
    });

    expect(mocks.setConnectorKey).toHaveBeenCalledWith(WalletConnectType.SOL);
    expect(mocks.setStorageChain).toHaveBeenCalledWith(901901901);
  });

  it("waits for an initializing fallback wallet after disconnect", async () => {
    mocks.solConnected = false;
    mocks.abstractConnected = false;
    mocks.privyConnected = true;
    mocks.privyEvmReady = false;
    mocks.privySolReady = false;
    const view = renderHook(() => useWallet());

    await act(async () => {
      await view.result.current.disconnect(WalletConnectType.EVM);
    });
    expect(mocks.setConnectorKey).toHaveBeenCalledWith("");

    mocks.privyEvm = {
      ...mocks.evm,
      label: "privy",
      chains: [{ id: 421614, namespace: ChainNamespace.evm }],
    };
    mocks.privyEvmReady = true;
    mocks.privySolReady = true;
    view.rerender();

    await waitFor(() =>
      expect(mocks.setConnectorKey).toHaveBeenCalledWith(
        WalletConnectType.PRIVY,
      ),
    );
    expect(mocks.setStorageChain).toHaveBeenCalledWith(421614);
  });
});
