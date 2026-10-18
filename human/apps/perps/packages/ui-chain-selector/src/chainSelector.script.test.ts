/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WALLET_CHAIN_CHANGE_PENDING_RESULT } from "@paxeer-network/types";
import { useChainSelectorScript } from "./chainSelector.script";
import { TChainItem } from "./type";

const mocks = vi.hoisted(() => ({
  setChain: vi.fn(),
  setStorageChain: vi.fn(),
  setCurrentChainId: vi.fn(),
  onChainChanged: vi.fn(),
  saveRecentChains: vi.fn(),
}));

vi.mock("@paxeer-network/hooks", () => ({
  useChains: () => [
    {
      mainnet: [],
      testnet: [],
    },
    { checkChainSupport: vi.fn(() => true) },
  ],
  useConfig: () => ({ get: () => "mainnet" }),
  useLocalStorage: () => [[], mocks.saveRecentChains],
  useStorageChain: () => ({ setStorageChain: mocks.setStorageChain }),
  useWalletConnector: () => ({
    connectedChain: { id: 42161, namespace: "EVM" },
    setChain: mocks.setChain,
  }),
}));

vi.mock("@paxeer-network/react-app", () => ({
  useAppContext: () => ({
    currentChainId: 42161,
    onChainChanged: mocks.onChainChanged,
    setCurrentChainId: mocks.setCurrentChainId,
    wrongNetwork: false,
  }),
}));

vi.mock("@paxeer-network/ui", () => ({
  usePaxeerNetworkTheme: () => ({
    getComponentTheme: () => ({ showTestnet: true }),
  }),
}));

const targetChain: TChainItem = {
  id: 901901901,
  name: "Solana",
  isTestnet: false,
};

describe("useChainSelectorScript", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("closes and rejects the selector when wallet connection is pending", async () => {
    const close = vi.fn();
    const resolve = vi.fn();
    const reject = vi.fn();
    const onChainChangeAfter = vi.fn();
    mocks.setChain.mockResolvedValue(WALLET_CHAIN_CHANGE_PENDING_RESULT);

    const { result } = renderHook(() =>
      useChainSelectorScript({
        close,
        reject,
        resolve,
        onChainChangeAfter,
      }),
    );

    await act(async () => {
      await result.current.onChainClick(targetChain);
    });

    expect(resolve).not.toHaveBeenCalled();
    expect(reject).toHaveBeenCalledWith(WALLET_CHAIN_CHANGE_PENDING_RESULT);
    expect(close).toHaveBeenCalledOnce();
    expect(onChainChangeAfter).toHaveBeenCalledWith(targetChain.id, {
      isTestnet: false,
      isWalletConnected: false,
      isWalletConnectionPending: true,
    });
    expect(mocks.onChainChanged).toHaveBeenCalledWith(targetChain.id, {
      isTestnet: false,
      isWalletConnected: false,
      isWalletConnectionPending: true,
    });
    expect(mocks.saveRecentChains).toHaveBeenCalledWith([targetChain.id]);
    expect(result.current.selectChainId).toBeUndefined();
  });

  it("preserves the successful chain switch behavior", async () => {
    const close = vi.fn();
    const resolve = vi.fn();
    const reject = vi.fn();
    const onChainChangeAfter = vi.fn();
    mocks.setChain.mockResolvedValue(true);

    const { result } = renderHook(() =>
      useChainSelectorScript({
        close,
        reject,
        resolve,
        onChainChangeAfter,
      }),
    );

    await act(async () => {
      await result.current.onChainClick(targetChain);
    });

    expect(resolve).toHaveBeenCalledOnce();
    expect(reject).not.toHaveBeenCalled();
    expect(close).toHaveBeenCalledOnce();
    expect(onChainChangeAfter).toHaveBeenCalledWith(targetChain.id, {
      isTestnet: false,
      isWalletConnected: true,
      isWalletConnectionPending: false,
    });
    expect(mocks.saveRecentChains).toHaveBeenCalledWith([targetChain.id]);
  });
});
