// @vitest-environment jsdom
import React, { useSyncExternalStore } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WalletConnectorPrivyProvider } from "./provider";
import { Network } from "./types";

const mocks = vi.hoisted(() => ({
  fallback: [
    {
      chain_id: "42161",
      currency_decimal: 18,
      currency_symbol: "ETH",
      explorer_base_url: "https://arbiscan.io",
      name: "Arbitrum",
      public_rpc_url: "https://arb1.arbitrum.io/rpc",
      vault_address: "0xvault",
    },
  ],
}));

type StoreState = {
  data: any[] | null;
  hydrated: boolean;
  fetchData: () => Promise<any[] | null>;
};

const createMockStore = (initialData: any[] | null) => {
  let state: StoreState;
  const listeners = new Set<() => void>();
  const setData = (data: any[] | null) => {
    state = { ...state, data };
    listeners.forEach((listener) => listener());
  };
  const fetchData = vi.fn(async () => {
    setData(mocks.fallback);
    return mocks.fallback;
  });
  state = { data: initialData, hydrated: true, fetchData };

  const useStore = (selector: (value: StoreState) => unknown) =>
    useSyncExternalStore(
      (listener) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
      () => selector(state),
    );
  useStore.getState = () => state;

  return { fetchData, useStore };
};

let mainnetStore = createMockStore(null);
let testnetStore = createMockStore(mocks.fallback);

vi.mock("@paxeer-network/hooks", () => ({
  useMainnetChainsStore: Object.assign(
    (selector: (value: StoreState) => unknown) =>
      mainnetStore.useStore(selector),
    { getState: () => mainnetStore.useStore.getState() },
  ),
  useTestnetChainsStore: Object.assign(
    (selector: (value: StoreState) => unknown) =>
      testnetStore.useStore(selector),
    { getState: () => testnetStore.useStore.getState() },
  ),
}));

vi.mock("@paxeer-network/ui", () => ({
  TooltipProvider: ({ children }: React.PropsWithChildren) => children,
}));

vi.mock("./main", () => ({
  Main: ({ children }: React.PropsWithChildren) => children,
}));
vi.mock("./providers/abstractWallet", () => ({
  AbstractWallet: ({ children }: React.PropsWithChildren) => children,
}));
vi.mock("./providers/privy", () => ({
  PrivyWallet: ({ children }: React.PropsWithChildren) => children,
}));
vi.mock("./providers/solana", () => ({
  SolanaWallet: ({ children }: React.PropsWithChildren) => children,
}));
vi.mock("./providers/wagmi", () => ({
  WagmiWallet: ({ children }: React.PropsWithChildren) => children,
}));

describe("WalletConnectorPrivyProvider chain-info fallback", () => {
  beforeEach(() => {
    mainnetStore = createMockStore(null);
    testnetStore = createMockStore(mocks.fallback);
  });

  it("mounts an interactive application shell after fallback data arrives", async () => {
    const onClick = vi.fn();
    render(
      <WalletConnectorPrivyProvider
        network={Network.testnet}
        wagmiConfig={{ connectors: [] } as any}
      >
        <button data-testid="app-shell" onClick={onClick}>
          Ready
        </button>
      </WalletConnectorPrivyProvider>,
    );

    const shell = await screen.findByTestId("app-shell");
    fireEvent.click(shell);

    expect(onClick).toHaveBeenCalledOnce();
    expect(mainnetStore.fetchData).toHaveBeenCalledOnce();
  });

  it("reuses hydrated broker cache without requesting generic data", async () => {
    mainnetStore = createMockStore(mocks.fallback);
    testnetStore = createMockStore(mocks.fallback);

    render(
      <WalletConnectorPrivyProvider
        network={Network.testnet}
        wagmiConfig={{ connectors: [] } as any}
      >
        <button data-testid="broker-cache-shell">Ready</button>
      </WalletConnectorPrivyProvider>,
    );

    expect(await screen.findByTestId("broker-cache-shell")).toBeTruthy();
    expect(mainnetStore.fetchData).not.toHaveBeenCalled();
    expect(testnetStore.fetchData).not.toHaveBeenCalled();
  });
});
