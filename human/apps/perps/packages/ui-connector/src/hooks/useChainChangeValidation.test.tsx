/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  AccountStatusEnum,
  ChainNamespace,
  SOLANA_MAINNET_CHAINID,
} from "@paxeer-network/types";
import { useChainChangeValidation } from "./useChainChangeValidation";

const mocks = vi.hoisted(() => {
  let validationHandler: ((status: AccountStatusEnum) => void) | undefined;

  return {
    accountState: {
      address: "0x123",
      status: 5 as AccountStatusEnum,
    },
    clearValidation: vi.fn(),
    connectedChain: {
      id: 42161,
      namespace: "EVM" as ChainNamespace,
    } as { id: number; namespace: ChainNamespace } | null,
    wallet: {
      accounts: [{ address: "0x123" }],
    } as { accounts: { address: string }[] } | null,
    waitForValidation: vi.fn((handler: (status: AccountStatusEnum) => void) => {
      validationHandler = handler;
    }),
    emitValidation(status: AccountStatusEnum) {
      const handler = validationHandler;
      validationHandler = undefined;
      handler?.(status);
    },
    reset() {
      validationHandler = undefined;
    },
  };
});

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({ state: mocks.accountState }),
  useWalletConnector: () => ({
    connectedChain: mocks.connectedChain,
    wallet: mocks.wallet,
  }),
}));

vi.mock("./useWalletConnectValidation", () => ({
  useWalletConnectValidation: () => ({
    clearValidation: mocks.clearValidation,
    waitForValidation: mocks.waitForValidation,
  }),
}));

describe("useChainChangeValidation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.reset();
    mocks.accountState.address = "0x123";
    mocks.accountState.status = AccountStatusEnum.EnableTrading;
    mocks.connectedChain = {
      id: 42161,
      namespace: ChainNamespace.evm,
    };
    mocks.wallet = { accounts: [{ address: "0x123" }] };
  });

  it("waits for the new account status when switching namespaces", () => {
    const onAccountValidated = vi.fn();
    const { result } = renderHook(() =>
      useChainChangeValidation({ onAccountValidated }),
    );

    act(() => result.current.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() => mocks.emitValidation(AccountStatusEnum.NotSignedIn));

    expect(mocks.waitForValidation).toHaveBeenCalledWith(expect.any(Function));
    expect(onAccountValidated).toHaveBeenCalledWith(
      AccountStatusEnum.NotSignedIn,
    );
  });

  it("does not validate twice when validation finishes before the switch callback", () => {
    const onAccountValidated = vi.fn();
    const { result } = renderHook(() =>
      useChainChangeValidation({ onAccountValidated }),
    );

    act(() => result.current.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() => mocks.emitValidation(AccountStatusEnum.NotSignedIn));
    act(() =>
      result.current.onChainChangeAfter(SOLANA_MAINNET_CHAINID, {
        isTestnet: false,
        isWalletConnected: true,
        isWalletConnectionPending: false,
      }),
    );

    expect(onAccountValidated).toHaveBeenCalledOnce();
    expect(onAccountValidated).toHaveBeenCalledWith(
      AccountStatusEnum.NotSignedIn,
    );
  });

  it("waits when the connector wallet and account addresses differ", () => {
    mocks.wallet = { accounts: [{ address: "0x456" }] };
    const onAccountValidated = vi.fn();
    const { result } = renderHook(() =>
      useChainChangeValidation({ onAccountValidated }),
    );

    act(() => result.current.onChainChangeBefore(10));

    expect(mocks.waitForValidation).toHaveBeenCalledWith(expect.any(Function));
  });

  it("uses the current status after a same-wallet namespace switch", () => {
    const onAccountValidated = vi.fn();
    const { result } = renderHook(() =>
      useChainChangeValidation({ onAccountValidated }),
    );

    act(() => result.current.onChainChangeBefore(10));
    act(() =>
      result.current.onChainChangeAfter(10, {
        isTestnet: false,
        isWalletConnected: true,
        isWalletConnectionPending: false,
      }),
    );

    expect(mocks.waitForValidation).not.toHaveBeenCalled();
    expect(onAccountValidated).toHaveBeenCalledWith(
      AccountStatusEnum.EnableTrading,
    );
  });

  it("keeps validation active while wallet connection is pending", () => {
    const onAccountValidated = vi.fn();
    const { result } = renderHook(() =>
      useChainChangeValidation({ onAccountValidated }),
    );

    act(() => result.current.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() =>
      result.current.onChainChangeAfter(SOLANA_MAINNET_CHAINID, {
        isTestnet: false,
        isWalletConnected: false,
        isWalletConnectionPending: true,
      }),
    );
    act(() => mocks.emitValidation(AccountStatusEnum.DisabledTrading));

    expect(mocks.clearValidation).toHaveBeenCalledOnce();
    expect(onAccountValidated).toHaveBeenCalledWith(
      AccountStatusEnum.DisabledTrading,
    );
  });

  it("clears validation after a failed chain switch", () => {
    const { result } = renderHook(() => useChainChangeValidation());

    act(() => result.current.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() =>
      result.current.onChainChangeAfter(SOLANA_MAINNET_CHAINID, {
        isTestnet: false,
        isWalletConnected: false,
        isWalletConnectionPending: false,
      }),
    );

    expect(mocks.clearValidation).toHaveBeenCalledTimes(2);
  });
});
