/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AccountStatusEnum } from "@paxeer-network/types";
import { WALLET_CONNECT_ABORTED } from "../constants/events";
import { useWalletConnectValidation } from "./useWalletConnectValidation";

const mocks = vi.hoisted(() => {
  const validationHandlers = new Set<(status: AccountStatusEnum) => void>();
  const eventHandlers = new Map<string, Set<() => void>>();

  return {
    account: {
      once: vi.fn(
        (_event: string, handler: (status: AccountStatusEnum) => void) => {
          validationHandlers.add(handler);
        },
      ),
      off: vi.fn(
        (_event: string, handler: (status: AccountStatusEnum) => void) => {
          validationHandlers.delete(handler);
        },
      ),
    },
    ee: {
      on: vi.fn((event: string, handler: () => void) => {
        const handlers = eventHandlers.get(event) ?? new Set();
        handlers.add(handler);
        eventHandlers.set(event, handlers);
      }),
      off: vi.fn((event: string, handler: () => void) => {
        eventHandlers.get(event)?.delete(handler);
      }),
      emit(event: string) {
        eventHandlers.get(event)?.forEach((handler) => handler());
      },
    },
    emitValidation(status: AccountStatusEnum) {
      const handlers = Array.from(validationHandlers);
      validationHandlers.clear();
      handlers.forEach((handler) => handler(status));
    },
    hasValidationHandler() {
      return validationHandlers.size > 0;
    },
    reset() {
      validationHandlers.clear();
      eventHandlers.clear();
    },
  };
});

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({ account: mocks.account }),
  useEventEmitter: () => mocks.ee,
}));

describe("useWalletConnectValidation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.reset();
  });

  it("replaces the previous validation listener", () => {
    const firstHandler = vi.fn();
    const secondHandler = vi.fn();
    const { result } = renderHook(() => useWalletConnectValidation());

    let disposeFirst!: () => void;
    act(() => {
      disposeFirst = result.current.waitForValidation(firstHandler);
      result.current.waitForValidation(secondHandler);
    });
    act(() => disposeFirst());
    act(() => mocks.emitValidation(AccountStatusEnum.NotSignedIn));

    expect(firstHandler).not.toHaveBeenCalled();
    expect(secondHandler).toHaveBeenCalledWith(AccountStatusEnum.NotSignedIn);
  });

  it("clears validation when wallet connection is aborted", () => {
    const handler = vi.fn();
    const { result } = renderHook(() => useWalletConnectValidation());

    act(() => result.current.waitForValidation(handler));
    act(() => mocks.ee.emit(WALLET_CONNECT_ABORTED));
    act(() => mocks.emitValidation(AccountStatusEnum.NotSignedIn));

    expect(handler).not.toHaveBeenCalled();
    expect(mocks.hasValidationHandler()).toBe(false);
  });

  it("clears validation when unmounted", () => {
    const handler = vi.fn();
    const { result, unmount } = renderHook(() => useWalletConnectValidation());

    act(() => result.current.waitForValidation(handler));
    unmount();
    act(() => mocks.emitValidation(AccountStatusEnum.NotSignedIn));

    expect(handler).not.toHaveBeenCalled();
    expect(mocks.hasValidationHandler()).toBe(false);
  });
});
