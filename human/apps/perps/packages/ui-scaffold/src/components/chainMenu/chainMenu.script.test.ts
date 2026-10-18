/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AccountStatusEnum } from "@paxeer-network/types";
import { useChainMenuScript } from "./chainMenu.script";

const mocks = vi.hoisted(() => ({
  completeChainChange: vi.fn(),
  prepareChainChange: vi.fn(),
}));

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({
    state: { status: AccountStatusEnum.EnableTrading },
  }),
  useConfig: () => "mainnet",
  useWalletConnector: () => ({
    connectedChain: { id: 42161, namespace: "EVM" },
  }),
}));

vi.mock("@paxeer-network/react-app", () => ({
  useAppContext: () => ({
    currentChainId: 42161,
    wrongNetwork: false,
    disabledConnect: false,
    setCurrentChainId: vi.fn(),
  }),
}));

vi.mock("@paxeer-network/ui-connector", () => ({
  useChainChangeValidation: () => ({
    onChainChangeBefore: mocks.prepareChainChange,
    onChainChangeAfter: mocks.completeChainChange,
  }),
}));

describe("useChainMenuScript", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("prepares validation and enters loading before a chain switch", () => {
    const { result } = renderHook(() => useChainMenuScript());

    act(() => result.current.onOpenChange(true));
    act(() => result.current.onChainChangeBefore(10));

    expect(result.current.open).toBe(false);
    expect(result.current.loading).toBe(true);
    expect(mocks.prepareChainChange).toHaveBeenCalledWith(10);
  });

  it("completes validation and clears loading after a chain switch", () => {
    const { result } = renderHook(() => useChainMenuScript());
    const chainChangeState = {
      isTestnet: false,
      isWalletConnected: true,
    };

    act(() => result.current.onChainChangeBefore(10));
    act(() => result.current.onChainChangeAfter(10, chainChangeState));

    expect(result.current.loading).toBe(false);
    expect(mocks.completeChainChange).toHaveBeenCalledWith(
      10,
      chainChangeState,
    );
  });
});
