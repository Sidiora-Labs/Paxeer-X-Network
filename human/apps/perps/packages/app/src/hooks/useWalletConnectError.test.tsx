/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useWalletConnectError } from "./useWalletConnectError";

const mocks = vi.hoisted(() => ({
  handlers: new Map<string, (data: any) => void>(),
  modalConfirm: vi.fn(),
  setManualLedgerAddress: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({ state: { connectWallet: undefined } }),
  useEventEmitter: () => ({
    on: (event: string, handler: (data: any) => void) =>
      mocks.handlers.set(event, handler),
    off: (event: string) => mocks.handlers.delete(event),
  }),
  useStorageLedgerAddress: () => ({
    setManualLedgerAddress: mocks.setManualLedgerAddress,
  }),
  useWalletConnector: () => ({
    namespace: "SOL",
    wallet: null,
  }),
}));

vi.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@paxeer-network/ui", () => ({
  modal: { confirm: mocks.modalConfirm },
  toast: { error: mocks.toastError },
}));

describe("useWalletConnectError", () => {
  beforeEach(() => {
    mocks.handlers.clear();
    vi.clearAllMocks();
  });

  it("does not offer Ledger fallback without an adapter name", () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    renderHook(() => useWalletConnectError());

    act(() => {
      mocks.handlers.get("wallet:sign-message-with-ledger-error")?.({
        message: "signing failed",
        userAddress: "address-a",
      });
    });

    expect(mocks.toastError).toHaveBeenCalledWith("signing failed");
    expect(mocks.modalConfirm).not.toHaveBeenCalled();
    expect(mocks.setManualLedgerAddress).not.toHaveBeenCalled();
    expect(consoleError).toHaveBeenCalled();

    consoleError.mockRestore();
  });
});
