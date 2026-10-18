/** @vitest-environment jsdom */
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  AccountStatusEnum,
  WALLET_CHAIN_CHANGE_PENDING_RESULT,
} from "@paxeer-network/types";
import { useAccountMenu } from "./useWidgetBuilder.script";

const mocks = vi.hoisted(() => {
  const eventListeners = new Map<string, Set<(...args: any[]) => void>>();

  return {
    account: {
      address: "0x123",
      disconnect: vi.fn(),
    },
    accountState: {
      status: 0,
      address: "0x123",
      connectWallet: undefined,
    },
    chainValidationOptions: [] as Array<{
      onAccountValidated?: (status: AccountStatusEnum) => void;
    }>,
    completeChainChange: vi.fn(),
    completeNetworkSwitch: vi.fn(),
    connectWallet: vi.fn(),
    disconnect: vi.fn(),
    handleAccountStatus: vi.fn(),
    modalShow: vi.fn(),
    prepareChainChange: vi.fn(),
    prepareNetworkSwitch: vi.fn(),
    setCurrentChainId: vi.fn(),
    toastSuccess: vi.fn(),
    ee: {
      on: vi.fn((event: string, handler: (...args: any[]) => void) => {
        const handlers = eventListeners.get(event) ?? new Set();
        handlers.add(handler);
        eventListeners.set(event, handlers);
      }),
      off: vi.fn((event: string, handler: (...args: any[]) => void) => {
        eventListeners.get(event)?.delete(handler);
      }),
    },
    reset() {
      eventListeners.clear();
    },
  };
});

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({
    account: mocks.account,
    state: mocks.accountState,
  }),
  useChains: () => [[], { findByChainId: vi.fn() }],
  useEventEmitter: () => mocks.ee,
  useWalletConnector: () => ({
    connectedChain: null,
    disconnect: mocks.disconnect,
  }),
}));

vi.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@paxeer-network/react-app", () => ({
  useAppContext: () => ({
    connectWallet: mocks.connectWallet,
    disabledConnect: false,
    wrongNetwork: true,
    setCurrentChainId: mocks.setCurrentChainId,
  }),
}));

vi.mock("@paxeer-network/ui", () => ({
  modal: { show: mocks.modalShow },
  toast: { success: mocks.toastSuccess },
  useScreen: () => ({ isMobile: false }),
}));

vi.mock("@paxeer-network/ui-chain-selector", () => ({
  ChainSelectorDialogId: "ChainSelectorDialogId",
  ChainSelectorSheetId: "ChainSelectorSheetId",
}));

vi.mock("@paxeer-network/ui-connector", () => ({
  useChainChangeValidation: (options: {
    onAccountValidated?: (status: AccountStatusEnum) => void;
  }) => {
    const index = mocks.chainValidationOptions.push(options) - 1;
    return index === 0
      ? {
          onChainChangeBefore: mocks.prepareChainChange,
          onChainChangeAfter: mocks.completeChainChange,
        }
      : {
          onChainChangeBefore: mocks.prepareNetworkSwitch,
          onChainChangeAfter: mocks.completeNetworkSwitch,
        };
  },
  useOnboardingModal: () => ({
    openOnboardingModal: vi.fn(),
    handleAccountStatus: mocks.handleAccountStatus,
  }),
}));

describe("useAccountMenu chain validation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.reset();
    mocks.chainValidationOptions.length = 0;
    mocks.connectWallet.mockResolvedValue({ wrongNetwork: true });
    mocks.modalShow.mockRejectedValue(WALLET_CHAIN_CHANGE_PENDING_RESULT);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("passes validation callbacks when connect opens the chain selector", async () => {
    const { result } = renderHook(() => useAccountMenu());

    await act(async () => {
      await result.current.connect();
    });

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith("ChainSelectorDialogId", {
        onChainChangeBefore: mocks.prepareChainChange,
        onChainChangeAfter: mocks.completeChainChange,
      }),
    );
  });

  it("keeps the wallet-connected toast for wrong-network recovery", () => {
    renderHook(() => useAccountMenu());

    act(() =>
      mocks.chainValidationOptions[0].onAccountValidated?.(
        AccountStatusEnum.EnableTrading,
      ),
    );

    expect(mocks.toastSuccess).toHaveBeenCalledOnce();
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "connector.walletConnected",
    );
  });

  it("passes validation callbacks when switching from the account menu", () => {
    const { result } = renderHook(() => useAccountMenu());

    act(() => result.current.onSwitchNetwork());

    expect(mocks.modalShow).toHaveBeenCalledWith("ChainSelectorDialogId", {
      bridgeLessOnly: false,
      isWrongNetwork: true,
      onChainChangeBefore: mocks.prepareNetworkSwitch,
      onChainChangeAfter: mocks.completeNetworkSwitch,
    });
    expect(mocks.chainValidationOptions[1].onAccountValidated).toBe(
      mocks.handleAccountStatus,
    );
  });

  it("shows only the network-switched toast after a manual switch", async () => {
    mocks.modalShow.mockResolvedValue({ chainId: 10, wrongNetwork: false });
    const { result } = renderHook(() => useAccountMenu());

    act(() => result.current.onSwitchNetwork());

    await waitFor(() =>
      expect(mocks.toastSuccess).toHaveBeenCalledWith(
        "connector.networkSwitched",
      ),
    );
    expect(mocks.toastSuccess).toHaveBeenCalledOnce();
    expect(mocks.setCurrentChainId).toHaveBeenCalledWith(10);
  });

  it("does not show a success toast while a manual switch is pending", async () => {
    const { result } = renderHook(() => useAccountMenu());

    act(() => result.current.onSwitchNetwork());
    await act(async () => {
      await Promise.resolve();
    });

    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });

  it("handles a pending wallet handoff without logging an error", async () => {
    const consoleLog = vi.spyOn(console, "log").mockImplementation(() => {});
    const { result } = renderHook(() => useAccountMenu());

    await act(async () => {
      await result.current.connect();
      await Promise.resolve();
    });

    expect(consoleLog).not.toHaveBeenCalled();
  });
});
