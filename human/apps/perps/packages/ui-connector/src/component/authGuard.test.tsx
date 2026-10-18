/** @vitest-environment jsdom */
import { cloneElement, type ReactElement } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  AccountStatusEnum,
  SOLANA_MAINNET_CHAINID,
  WALLET_CHAIN_CHANGE_PENDING_RESULT,
} from "@paxeer-network/types";
import { ChainSelectorDialogId } from "@paxeer-network/ui-chain-selector";
import { WALLET_CONNECT_ABORTED } from "../constants/events";
import { AuthGuard } from "./authGuard";
import { WalletConnectorModalId } from "./walletConnector";

const mocks = vi.hoisted(() => {
  const eventListeners = new Map<string, Set<(...args: any[]) => void>>();
  const ee = {
    on: vi.fn((event: string, handler: (...args: any[]) => void) => {
      const handlers = eventListeners.get(event) ?? new Set();
      handlers.add(handler);
      eventListeners.set(event, handlers);
    }),
    off: vi.fn((event: string, handler: (...args: any[]) => void) => {
      eventListeners.get(event)?.delete(handler);
    }),
    emit: vi.fn((event: string, ...args: any[]) => {
      eventListeners.get(event)?.forEach((handler) => handler(...args));
    }),
  };
  let validateEndHandlers: Array<{
    handler: (status: AccountStatusEnum) => void;
    once: boolean;
  }> = [];
  const account = {
    address: undefined as string | undefined,
    on: vi.fn((event: string, handler: (status: AccountStatusEnum) => void) => {
      if (event === "validate:end") {
        validateEndHandlers.push({ handler, once: false });
      }
    }),
    once: vi.fn(
      (event: string, handler: (status: AccountStatusEnum) => void) => {
        if (event === "validate:end") {
          validateEndHandlers.push({ handler, once: true });
        }
      },
    ),
    off: vi.fn(
      (event: string, handler: (status: AccountStatusEnum) => void) => {
        if (event === "validate:end") {
          validateEndHandlers = validateEndHandlers.filter(
            (listener) => listener.handler !== handler,
          );
        }
      },
    ),
  };

  return {
    ee,
    eventListeners,
    account,
    accountState: {
      status: 0 as AccountStatusEnum,
      validating: false,
      address: undefined as string | undefined,
    },
    connectWallet: vi.fn(),
    connectedChain: {
      id: 42161,
      namespace: "EVM",
    },
    modalShow: vi.fn(),
    toastSuccess: vi.fn(),
    wallet: {
      accounts: [{ address: "0x123" }],
    },
    wrongNetwork: false,
    emitValidateEnd(status: AccountStatusEnum) {
      const listeners = [...validateEndHandlers];
      validateEndHandlers = validateEndHandlers.filter(
        (listener) => !listener.once,
      );
      listeners.forEach((listener) => listener.handler(status));
    },
    resetListeners() {
      validateEndHandlers = [];
    },
  };
});

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({
    account: mocks.account,
    state: mocks.accountState,
  }),
  useEventEmitter: () => mocks.ee,
  useWalletConnector: () => ({
    connectedChain: mocks.connectedChain,
    wallet: mocks.wallet,
  }),
}));

vi.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@paxeer-network/react-app", () => ({
  useAppContext: () => ({
    connectWallet: mocks.connectWallet,
    disabledConnect: false,
    wrongNetwork: mocks.wrongNetwork,
  }),
}));

vi.mock("@paxeer-network/ui", async (importOriginal) => {
  const original = await importOriginal<typeof import("@paxeer-network/ui")>();
  return {
    ...original,
    modal: { show: mocks.modalShow },
    toast: { success: mocks.toastSuccess },
    useScreen: () => ({ isMobile: false }),
  };
});

describe("AuthGuard manual wallet onboarding", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.eventListeners.clear();
    mocks.resetListeners();
    mocks.accountState.status = AccountStatusEnum.NotConnected;
    mocks.accountState.validating = false;
    mocks.accountState.address = undefined;
    mocks.account.address = undefined;
    mocks.wrongNetwork = false;
    mocks.connectWallet.mockResolvedValue(null);
    mocks.modalShow.mockResolvedValue({ wrongNetwork: false });
  });

  afterEach(() => {
    cleanup();
  });

  it.each([
    AccountStatusEnum.NotSignedIn,
    AccountStatusEnum.SignedIn,
    AccountStatusEnum.DisabledTrading,
  ])("opens onboarding once for account status %s", async (status) => {
    mocks.connectWallet.mockResolvedValue({ status, wrongNetwork: false });
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() => expect(mocks.modalShow).toHaveBeenCalledTimes(1));
    expect(mocks.modalShow).toHaveBeenCalledWith(
      WalletConnectorModalId,
      expect.objectContaining({ initAccountState: status }),
    );
  });

  it("keeps the modal title in sync with the live account status", async () => {
    mocks.connectWallet.mockResolvedValue({
      status: AccountStatusEnum.NotSignedIn,
      wrongNetwork: false,
    });
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));
    await waitFor(() => expect(mocks.modalShow).toHaveBeenCalledTimes(1));

    const title = mocks.modalShow.mock.calls[0][1].title as ReactElement;
    const titleView = render(cloneElement(title));
    expect(screen.getByText("connector.createAccount")).toBeTruthy();

    mocks.accountState.status = AccountStatusEnum.DisabledTrading;
    titleView.rerender(cloneElement(title));

    expect(screen.queryByText("connector.createAccount")).toBeNull();
    expect(screen.getByText("connector.enableTrading")).toBeTruthy();
  });

  it("uses the requested onboarding status while account state catches up", async () => {
    mocks.connectWallet.mockResolvedValue({
      status: AccountStatusEnum.DisabledTrading,
      wrongNetwork: false,
    });
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));
    await waitFor(() => expect(mocks.modalShow).toHaveBeenCalledTimes(1));

    const title = mocks.modalShow.mock.calls[0][1].title as ReactElement;
    render(cloneElement(title));

    expect(screen.queryByText("connector.createAccount")).toBeNull();
    expect(screen.getByText("connector.enableTrading")).toBeTruthy();
  });

  it("does not open onboarding for an enabled account", async () => {
    mocks.connectWallet.mockResolvedValue({
      status: AccountStatusEnum.EnableTrading,
      wrongNetwork: false,
    });
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() => expect(mocks.connectWallet).toHaveBeenCalledTimes(1));
    expect(mocks.modalShow).not.toHaveBeenCalled();
  });

  it("does not open onboarding when wallet selection is cancelled", async () => {
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() => expect(mocks.connectWallet).toHaveBeenCalledTimes(1));
    expect(mocks.modalShow).not.toHaveBeenCalled();
  });

  it("does not open onboarding when connection rejects", async () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    mocks.connectWallet.mockRejectedValue(new Error("connect failed"));
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() => expect(consoleError).toHaveBeenCalledTimes(1));
    expect(mocks.modalShow).not.toHaveBeenCalled();
    consoleError.mockRestore();
  });

  it("handles each later wallet selection from its direct result", async () => {
    mocks.connectWallet
      .mockResolvedValueOnce({
        status: AccountStatusEnum.EnableTrading,
        wrongNetwork: false,
      })
      .mockResolvedValueOnce({
        status: AccountStatusEnum.NotSignedIn,
        wrongNetwork: false,
      });
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));
    await waitFor(() => expect(mocks.connectWallet).toHaveBeenCalledTimes(1));
    expect(mocks.modalShow).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("connector.connectWallet"));
    await waitFor(() => expect(mocks.modalShow).toHaveBeenCalledTimes(1));
    expect(mocks.connectWallet).toHaveBeenCalledTimes(2);
  });

  it("waits for validation after switching from a wrong network", async () => {
    mocks.connectWallet.mockResolvedValue({ wrongNetwork: true });
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        ChainSelectorDialogId,
        expect.any(Object),
      ),
    );

    const chainSelectorOptions = mocks.modalShow.mock.calls[0][1];
    act(() => chainSelectorOptions.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() => mocks.emitValidateEnd(AccountStatusEnum.DisabledTrading));

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        WalletConnectorModalId,
        expect.any(Object),
      ),
    );
    expect(mocks.modalShow).toHaveBeenCalledTimes(2);
  });

  it("keeps waiting for validation when wallet connection is pending", async () => {
    mocks.connectWallet.mockResolvedValue({ wrongNetwork: true });
    mocks.modalShow.mockRejectedValueOnce(WALLET_CHAIN_CHANGE_PENDING_RESULT);
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        ChainSelectorDialogId,
        expect.any(Object),
      ),
    );

    const chainSelectorOptions = mocks.modalShow.mock.calls[0][1];
    act(() => chainSelectorOptions.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() =>
      chainSelectorOptions.onChainChangeAfter(SOLANA_MAINNET_CHAINID, {
        isTestnet: false,
        isWalletConnected: false,
        isWalletConnectionPending: true,
      }),
    );
    act(() => mocks.emitValidateEnd(AccountStatusEnum.DisabledTrading));

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        WalletConnectorModalId,
        expect.any(Object),
      ),
    );
  });

  it("stops waiting for validation when wallet connection is aborted", async () => {
    mocks.connectWallet.mockResolvedValue({ wrongNetwork: true });
    mocks.modalShow.mockRejectedValueOnce(WALLET_CHAIN_CHANGE_PENDING_RESULT);
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.connectWallet"));

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        ChainSelectorDialogId,
        expect.any(Object),
      ),
    );

    const chainSelectorOptions = mocks.modalShow.mock.calls[0][1];
    act(() => chainSelectorOptions.onChainChangeBefore(SOLANA_MAINNET_CHAINID));
    act(() => mocks.ee.emit(WALLET_CONNECT_ABORTED));
    act(() => mocks.emitValidateEnd(AccountStatusEnum.DisabledTrading));

    expect(mocks.account.off).toHaveBeenCalledWith(
      "validate:end",
      expect.any(Function),
    );
    expect(mocks.modalShow).toHaveBeenCalledTimes(1);
  });

  it("uses the current status without leaving a same-wallet listener", async () => {
    mocks.accountState.status = AccountStatusEnum.NotSignedIn;
    mocks.accountState.address = "0x123";
    mocks.account.address = "0x123";
    mocks.wrongNetwork = true;
    render(<AuthGuard />);

    fireEvent.click(screen.getByText("connector.wrongNetwork"));

    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        ChainSelectorDialogId,
        expect.any(Object),
      ),
    );

    const chainSelectorOptions = mocks.modalShow.mock.calls[0][1];
    act(() => chainSelectorOptions.onChainChangeBefore(10));
    act(() =>
      chainSelectorOptions.onChainChangeAfter(10, {
        isTestnet: false,
        isWalletConnected: true,
        isWalletConnectionPending: false,
      }),
    );

    expect(mocks.account.once).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(mocks.modalShow).toHaveBeenCalledWith(
        WalletConnectorModalId,
        expect.any(Object),
      ),
    );
  });

  it("does not let AuthGuard instances compete for OAuth resume results", () => {
    render(
      <>
        <AuthGuard networkId="mainnet" />
        <AuthGuard networkId="testnet" bridgeLessOnly />
      </>,
    );
    const result = {
      intentId: "google-flow",
      status: AccountStatusEnum.NotSignedIn,
      wrongNetwork: false,
      handled: false,
    };
    act(() => mocks.ee.emit("wallet:connect-oauth-resume-result", result));

    expect(mocks.modalShow).not.toHaveBeenCalled();
    expect(mocks.connectWallet).not.toHaveBeenCalled();
    expect(result.handled).toBe(false);
  });
});
