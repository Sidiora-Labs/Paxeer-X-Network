// @vitest-environment jsdom
import React, { useContext } from "react";
import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WalletConnectorContext } from "@paxeer-network/hooks";
import { ChainNamespace } from "@paxeer-network/types";
import { WALLET_CONNECT_ABORTED } from "@paxeer-network/ui-connector";
import {
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_PROVIDER_CANCEL,
  WALLET_CONNECT_PROVIDER_START,
  WALLET_CONNECT_WALLET_SELECTED,
} from "./connectEvents";
import { Main } from "./main";
import {
  getOAuthConnectIntent,
  markOAuthConnectIntent,
  markOAuthConnectIntentReturned,
} from "./oauthConnectIntent";
import { WalletConnectType } from "./types";

const mocks = vi.hoisted(() => {
  const listeners = new Map<string, Set<(...args: any[]) => void>>();
  const ee = {
    on: vi.fn((event: string, handler: (...args: any[]) => void) => {
      const handlers = listeners.get(event) ?? new Set();
      handlers.add(handler);
      listeners.set(event, handlers);
    }),
    off: vi.fn((event: string, handler: (...args: any[]) => void) => {
      listeners.get(event)?.delete(handler);
    }),
    emit: vi.fn((event: string, ...args: any[]) => {
      listeners.get(event)?.forEach((handler) => handler(...args));
    }),
  };

  return {
    ee,
    listeners,
    drawerProps: null as any,
    setOpenConnectDrawer: vi.fn(),
    setTargetWalletType: vi.fn(),
    walletState: {
      wallet: null as any,
      walletType: null as WalletConnectType | null,
      connectedChain: undefined,
      setChain: vi.fn(),
      namespace: null,
      onDisconnect: vi.fn(),
      restoreConnectorState: vi.fn(),
    },
  };
});

vi.mock("@paxeer-network/hooks", async () => {
  const React = await import("react");
  return {
    useEventEmitter: () => mocks.ee,
    WalletConnectorContext: React.createContext<any>(null),
  };
});

vi.mock("./hooks/useWallet", () => ({
  useWallet: () => mocks.walletState,
}));

vi.mock("./provider", () => ({
  useWalletConnectorPrivy: () => ({
    openConnectDrawer: false,
    setOpenConnectDrawer: mocks.setOpenConnectDrawer,
    setTargetWalletType: mocks.setTargetWalletType,
  }),
}));

vi.mock("./components/connectDrawer", () => ({
  ConnectDrawer: (props: any) => {
    mocks.drawerProps = props;
    return null;
  },
}));

vi.mock("./injectUsercenter", () => ({}));

const createWallet = (address: string) => ({
  label: "test",
  icon: "",
  provider: {},
  accounts: [{ address }],
  chains: [{ id: 1, namespace: ChainNamespace.evm }],
});

let contextValue: any;

const ContextProbe = () => {
  contextValue = useContext(WalletConnectorContext);
  return null;
};

describe("Privy connector Main", () => {
  beforeEach(() => {
    mocks.listeners.clear();
    mocks.ee.emit.mockClear();
    mocks.setOpenConnectDrawer.mockClear();
    mocks.setTargetWalletType.mockClear();
    mocks.walletState.restoreConnectorState.mockClear();
    mocks.walletState.wallet = null;
    mocks.walletState.walletType = null;
    window.sessionStorage.clear();
    contextValue = null;
  });

  it("resolves a manual connection with the aggregated wallet", async () => {
    const view = render(
      <Main>
        <ContextProbe />
      </Main>,
    );
    const wallet = createWallet("0x2");
    let promise: Promise<any>;

    markOAuthConnectIntent("google");

    act(() => {
      promise = contextValue.connect();
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.PRIVY,
      });
    });
    await waitFor(() => expect(contextValue.connecting).toBe(true));

    mocks.walletState.wallet = wallet;
    mocks.walletState.walletType = WalletConnectType.PRIVY;
    view.rerender(
      <Main>
        <ContextProbe />
      </Main>,
    );

    await expect(promise!).resolves.toEqual([wallet]);
    expect(getOAuthConnectIntent()).toBeNull();
    await waitFor(() => expect(contextValue.connecting).toBe(false));
    expect(mocks.setOpenConnectDrawer).toHaveBeenLastCalledWith(false);

    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
        walletType: WalletConnectType.PRIVY,
      });
    });
    expect(mocks.walletState.restoreConnectorState).not.toHaveBeenCalled();
  });

  it("resolves selection close and provider cancellation with an empty list", async () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    const selection = contextValue.connect();
    act(() => mocks.drawerProps.onChangeOpen(false));
    await expect(selection).resolves.toEqual([]);
    expect(mocks.ee.emit).toHaveBeenCalledWith(WALLET_CONNECT_ABORTED);

    mocks.ee.emit.mockClear();
    const provider = contextValue.connect();
    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.SOL,
      });
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
        walletType: WalletConnectType.SOL,
      });
    });
    await expect(provider).resolves.toEqual([]);
    expect(mocks.ee.emit).toHaveBeenCalledWith(WALLET_CONNECT_ABORTED);
  });

  it("restores the previous connector after provider cancellation", async () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    const promise = contextValue.connect();
    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.SOL,
        previousConnectorKey: WalletConnectType.EVM,
        previousChainId: 421614,
      });
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
        walletType: WalletConnectType.SOL,
      });
    });

    await expect(promise).resolves.toEqual([]);
    expect(mocks.walletState.restoreConnectorState).toHaveBeenCalledWith(
      WalletConnectType.EVM,
      421614,
    );
  });

  it("restores a chain-selector connection without a pending connect request", () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.SOL,
        previousConnectorKey: WalletConnectType.EVM,
        previousChainId: 421614,
      });
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_CANCEL, {
        walletType: WalletConnectType.SOL,
      });
    });

    expect(mocks.walletState.restoreConnectorState).toHaveBeenCalledWith(
      WalletConnectType.EVM,
      421614,
    );
  });

  it("restores a chain-selector connection when the drawer closes", () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.SOL,
        previousConnectorKey: WalletConnectType.EVM,
        previousChainId: 421614,
      });
      mocks.drawerProps.onChangeOpen(false);
    });

    expect(mocks.walletState.restoreConnectorState).toHaveBeenCalledWith(
      WalletConnectType.EVM,
      421614,
    );
  });

  it("restores the previous connector after a provider error", async () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    const promise = contextValue.connect();
    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.ABSTRACT,
        previousConnectorKey: WalletConnectType.EVM,
        previousChainId: 421614,
      });
      mocks.ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.ABSTRACT,
        message: "connect failed",
      });
    });

    await expect(promise).rejects.toThrow("connect failed");
    expect(mocks.walletState.restoreConnectorState).toHaveBeenCalledWith(
      WalletConnectType.EVM,
      421614,
    );
    expect(mocks.ee.emit).toHaveBeenCalledWith(WALLET_CONNECT_ABORTED);
  });

  it("does not cancel a provider when the drawer closes programmatically", async () => {
    const view = render(
      <Main>
        <ContextProbe />
      </Main>,
    );
    const wallet = createWallet("0x3");
    const promise = contextValue.connect();

    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.PRIVY,
      });
      mocks.drawerProps.onChangeOpen(false);
    });
    expect(mocks.ee.emit).not.toHaveBeenCalledWith(WALLET_CONNECT_ABORTED);

    mocks.walletState.wallet = wallet;
    mocks.walletState.walletType = WalletConnectType.PRIVY;
    view.rerender(
      <Main>
        <ContextProbe />
      </Main>,
    );
    await expect(promise).resolves.toEqual([wallet]);
  });

  it("cancels a wallet-prompt connection when the user closes the drawer and reconnects afterwards", async () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    const abandoned = contextValue.connect();
    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.EVM,
        previousConnectorKey: WalletConnectType.SOL,
        previousChainId: 900901,
      });
      mocks.drawerProps.onChangeOpen(false);
    });
    await expect(abandoned).resolves.toEqual([]);
    await waitFor(() => expect(contextValue.connecting).toBe(false));
    expect(mocks.walletState.restoreConnectorState).toHaveBeenCalledWith(
      WalletConnectType.SOL,
      900901,
    );

    // The cancelled request must not swallow the next connect() call.
    contextValue.connect();
    expect(mocks.setOpenConnectDrawer).toHaveBeenLastCalledWith(true);
  });

  it("rejects provider errors", async () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );
    const promise = contextValue.connect();

    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.ABSTRACT,
      });
      mocks.ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.ABSTRACT,
        message: "connect failed",
      });
    });

    await expect(promise).rejects.toThrow("connect failed");
  });

  it("reuses a pending request and resolves existing wallet selections", async () => {
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );
    const wallet = createWallet("0x4");
    const first = contextValue.connect();
    const second = contextValue.connect();

    expect(second).toBe(first);
    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledTimes(1);
    expect(mocks.setOpenConnectDrawer).toHaveBeenCalledWith(true);
    act(() => mocks.ee.emit(WALLET_CONNECT_WALLET_SELECTED, wallet));
    await expect(first).resolves.toEqual([wallet]);
  });

  it("resolves autoSelect and unmount cleanup with an empty list", async () => {
    const view = render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    await expect(contextValue.connect({ autoSelect: true })).resolves.toEqual(
      [],
    );
    expect(mocks.setOpenConnectDrawer).not.toHaveBeenCalled();

    const pending = contextValue.connect();
    const intent = markOAuthConnectIntent("google");
    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.PRIVY,
        previousConnectorKey: WalletConnectType.EVM,
        previousChainId: 421614,
      });
    });
    view.unmount();
    await expect(pending).resolves.toEqual([]);
    expect(getOAuthConnectIntent()).toEqual(intent);
    expect(mocks.walletState.restoreConnectorState).not.toHaveBeenCalled();
  });

  it("restores a non-Privy connector snapshot during unmount", async () => {
    const view = render(
      <Main>
        <ContextProbe />
      </Main>,
    );
    const pending = contextValue.connect();

    act(() => {
      mocks.ee.emit(WALLET_CONNECT_PROVIDER_START, {
        walletType: WalletConnectType.SOL,
        previousConnectorKey: WalletConnectType.EVM,
        previousChainId: 421614,
      });
    });
    view.unmount();

    await expect(pending).resolves.toEqual([]);
    expect(mocks.walletState.restoreConnectorState).toHaveBeenCalledWith(
      WalletConnectType.EVM,
      421614,
    );
  });

  it("clears a returned OAuth intent when wallet initialization fails", () => {
    markOAuthConnectIntent("google");
    markOAuthConnectIntentReturned();
    render(
      <Main>
        <ContextProbe />
      </Main>,
    );

    act(() =>
      mocks.ee.emit(WALLET_CONNECT_ERROR, {
        walletType: WalletConnectType.PRIVY,
        message: "wallet initialization failed",
      }),
    );

    expect(getOAuthConnectIntent()).toBeNull();
  });
});
