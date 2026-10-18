/** @vitest-environment jsdom */
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AccountStatusEnum, ChainNamespace } from "@paxeer-network/types";
import { useWalletStateHandle } from "./useWalletStateHandle";

const mocks = vi.hoisted(() => {
  const handlers = new Map<string, (...args: any[]) => any>();
  const emit = vi.fn((event: string, ...args: any[]) => {
    return handlers.get(event)?.(...args);
  });

  return {
    handlers,
    emit,
    checkChainSupport: vi.fn(() => true),
    connect: vi.fn(),
    disconnect: vi.fn(),
    setAddress: vi.fn(),
    switchChainId: vi.fn(),
    setStorageChain: vi.fn(),
    track: vi.fn(),
    setTrackUserId: vi.fn(),
    accountState: {
      status: 0,
      validating: false,
    },
    account: {
      address: undefined as string | undefined,
      chainId: undefined as number | undefined,
      accountId: undefined as string | undefined,
      disconnect: vi.fn(),
      setAddress: vi.fn(),
      switchChainId: vi.fn(),
    },
    wallet: {
      label: "privy",
      icon: "",
      provider: {},
      accounts: [{ address: "0xgoogle" }],
      chains: [{ id: 1, namespace: "EVM" }],
    },
  };
});

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({
    account: mocks.account,
    state: mocks.accountState,
  }),
  useChains: () => [[], { checkChainSupport: mocks.checkChainSupport }],
  useConfig: (key: string) => (key === "networkId" ? "testnet" : "broker"),
  useEventEmitter: () => ({
    emit: mocks.emit,
    on: (event: string, handler: (...args: any[]) => any) =>
      mocks.handlers.set(event, handler),
    off: (event: string) => mocks.handlers.delete(event),
  }),
  useKeyStore: () => ({ getAddress: () => undefined }),
  useStorageChain: () => ({
    storageChain: null,
    setStorageChain: mocks.setStorageChain,
  }),
  useTrack: () => ({
    track: mocks.track,
    setTrackUserId: mocks.setTrackUserId,
  }),
  useWalletConnector: () => ({
    wallet: mocks.wallet,
    connect: mocks.connect,
    connectedChain: mocks.wallet.chains[0],
    disconnect: mocks.disconnect,
    namespace: ChainNamespace.evm,
  }),
}));

vi.mock("@paxeer-network/utils", () => ({
  parseChainIdToNumber: (value: number) => value,
  praseChainIdToNumber: (value: number) => value,
  windowGuard: (callback: () => void) => callback(),
}));

vi.mock("./useLinkDevice", () => ({
  getLinkDeviceData: () => null,
}));

const setReturnedIntent = (intentId: string) => {
  window.sessionStorage.setItem(
    "paxeer-network:privy-oauth-connect-intent",
    JSON.stringify({
      id: intentId,
      loginMethod: "google",
      phase: "returned",
      expiresAt: Date.now() + 60_000,
    }),
  );
};

const setRedirectingIntent = (intentId: string) => {
  window.sessionStorage.setItem(
    "paxeer-network:privy-oauth-connect-intent",
    JSON.stringify({
      id: intentId,
      loginMethod: "google",
      phase: "redirecting",
      expiresAt: Date.now() + 60_000,
    }),
  );
};

describe("useWalletStateHandle OAuth resume", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.handlers.clear();
    mocks.checkChainSupport.mockReturnValue(true);
    mocks.account.setAddress.mockResolvedValue(AccountStatusEnum.NotSignedIn);
    mocks.account.address = undefined;
    mocks.account.chainId = undefined;
    mocks.accountState.status = AccountStatusEnum.NotConnected;
    mocks.accountState.validating = false;
    window.sessionStorage.clear();
  });

  it("automatically validates a returned OAuth wallet when it becomes ready", async () => {
    setReturnedIntent("google-flow");
    renderHook(() => useWalletStateHandle({}));

    await waitFor(() => expect(mocks.account.setAddress).toHaveBeenCalled());

    expect(mocks.account.setAddress).toHaveBeenCalledWith(
      "0xgoogle",
      expect.objectContaining({
        provider: mocks.wallet.provider,
        chain: { id: 1, namespace: ChainNamespace.evm },
      }),
    );
    expect(mocks.emit).toHaveBeenCalledWith(
      "wallet:connect-oauth-resume-result",
      expect.objectContaining({
        intentId: "google-flow",
        status: AccountStatusEnum.NotSignedIn,
        wrongNetwork: false,
      }),
    );
    expect(
      window.sessionStorage.getItem("paxeer-network:privy-oauth-connect-intent"),
    ).toBeNull();
  });

  it("resumes when OAuth returns after the wallet is already ready", async () => {
    setRedirectingIntent("google-flow");
    renderHook(() => useWalletStateHandle({}));

    expect(mocks.account.setAddress).not.toHaveBeenCalled();
    mocks.emit.mockClear();
    setReturnedIntent("google-flow");

    act(() => {
      mocks.emit("wallet:connect-oauth-returned", {
        intentId: "google-flow",
      });
    });

    await waitFor(() =>
      expect(mocks.emit).toHaveBeenCalledWith(
        "wallet:connect-oauth-resume-result",
        expect.objectContaining({
          intentId: "google-flow",
          status: AccountStatusEnum.NotSignedIn,
          wrongNetwork: false,
        }),
      ),
    );
    expect(mocks.account.setAddress).toHaveBeenCalledTimes(1);
  });

  it("reports a wrong network before setting the account address", async () => {
    setReturnedIntent("google-flow");
    mocks.checkChainSupport.mockReturnValue(false);
    renderHook(() => useWalletStateHandle({}));

    await waitFor(() =>
      expect(mocks.emit).toHaveBeenCalledWith(
        "wallet:connect-oauth-resume-result",
        expect.objectContaining({
          intentId: "google-flow",
          wrongNetwork: true,
        }),
      ),
    );

    expect(mocks.account.setAddress).not.toHaveBeenCalled();
    expect(mocks.emit).toHaveBeenCalledWith(
      "wallet:connect-oauth-resume-result",
      expect.objectContaining({
        intentId: "google-flow",
        wrongNetwork: true,
      }),
    );
  });

  it("ignores a stale redirect that did not complete", async () => {
    setRedirectingIntent("stale-flow");
    renderHook(() => useWalletStateHandle({}));

    await act(async () => {
      await mocks.handlers.get("wallet:connect-oauth-resume")?.({
        intentId: "stale-flow",
        wallet: mocks.wallet,
      });
    });

    expect(mocks.emit).not.toHaveBeenCalledWith(
      "wallet:connect-oauth-resume-result",
      expect.anything(),
    );
  });

  it("expires a stale returned intent and continues normal wallet sync", async () => {
    window.sessionStorage.setItem(
      "paxeer-network:privy-oauth-connect-intent",
      JSON.stringify({
        id: "expired-flow",
        loginMethod: "google",
        phase: "returned",
        expiresAt: Date.now() - 1,
      }),
    );

    renderHook(() => useWalletStateHandle({}));

    await waitFor(() => expect(mocks.account.setAddress).toHaveBeenCalled());
    expect(
      window.sessionStorage.getItem("paxeer-network:privy-oauth-connect-intent"),
    ).toBeNull();
  });
});
