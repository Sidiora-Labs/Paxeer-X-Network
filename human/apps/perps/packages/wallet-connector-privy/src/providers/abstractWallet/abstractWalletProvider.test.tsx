// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  WALLET_CONNECT_ERROR,
  WALLET_CONNECT_PROVIDER_CANCEL,
} from "../../connectEvents";
import { WalletConnectType } from "../../types";
import {
  AbstractWalletProvider,
  useAbstractWallet,
} from "./abstractWalletProvider";

const mocks = vi.hoisted(() => ({
  connect: vi.fn(),
  emit: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
  connector: { id: "xyz.abs.privy" },
}));

vi.mock("@abstract-foundation/agw-react", () => ({
  useAbstractClient: () => ({ data: null }),
  useGlobalWalletSignerAccount: () => ({ address: undefined }),
  useLoginWithAbstract: () => ({
    login: mocks.login,
    logout: mocks.logout,
  }),
}));

vi.mock("wagmi", () => ({
  useAccount: () => ({ connector: null }),
  useConnect: () => ({
    connect: mocks.connect,
    connectors: [mocks.connector],
  }),
}));

vi.mock("@paxeer-network/hooks", () => ({
  useEventEmitter: () => ({ emit: mocks.emit }),
}));

vi.mock("@paxeer-network/utils", () => ({
  windowGuard: vi.fn(),
}));

vi.mock("../../provider", () => ({
  useWalletConnectorPrivy: () => ({ network: "testnet" }),
}));

const ContextProbe = () => {
  const { connect } = useAbstractWallet();
  return <button onClick={connect}>Connect Abstract</button>;
};

describe("AbstractWalletProvider connection errors", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("reports a Wagmi user rejection as provider cancellation", () => {
    render(
      <AbstractWalletProvider disabled={false}>
        <ContextProbe />
      </AbstractWalletProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Connect Abstract" }));
    const callbacks = mocks.connect.mock.calls[0][1];
    act(() => callbacks.onError({ code: 4001 }));

    expect(mocks.emit).toHaveBeenCalledWith(WALLET_CONNECT_PROVIDER_CANCEL, {
      walletType: WalletConnectType.ABSTRACT,
    });
  });

  it("reports a Wagmi connection failure as a real error", () => {
    render(
      <AbstractWalletProvider disabled={false}>
        <ContextProbe />
      </AbstractWalletProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Connect Abstract" }));
    const callbacks = mocks.connect.mock.calls[0][1];
    act(() => callbacks.onError(new Error("abstract failed")));

    expect(mocks.emit).toHaveBeenCalledWith(WALLET_CONNECT_ERROR, {
      walletType: WalletConnectType.ABSTRACT,
      message: "abstract failed",
    });
  });
});
