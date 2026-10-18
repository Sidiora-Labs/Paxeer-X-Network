/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAssetconvertEvent } from "./useAssetconvertEvent";

const mocks = vi.hoisted(() => ({
  emit: vi.fn(),
  onMessage: undefined as ((data: Record<string, unknown>) => void) | undefined,
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  unsubscribe: vi.fn(),
}));

vi.mock("@paxeer-network/hooks", () => ({
  useEventEmitter: () => ({ emit: mocks.emit }),
  useWS: () => ({
    privateSubscribe: (
      _request: unknown,
      options: { onMessage: (data: Record<string, unknown>) => void },
    ) => {
      mocks.onMessage = options.onMessage;
      return mocks.unsubscribe;
    },
  }),
}));

vi.mock("@paxeer-network/i18n", () => ({
  i18n: { t: (key: string) => key },
}));

vi.mock("@paxeer-network/ui", () => ({
  toast: {
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
}));

vi.mock("@paxeer-network/utils", () => ({
  getTimestamp: () => 123,
}));

describe("useAssetconvertEvent", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.onMessage = undefined;
  });

  it("shows a failure toast when convertedQty is zero", () => {
    renderHook(() => useAssetconvertEvent());
    const data = { convertId: 131235, convertedQty: 0 };

    act(() => mocks.onMessage?.(data));

    expect(mocks.toastError).toHaveBeenCalledWith("transfer.convert.failed");
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
    expect(mocks.emit).toHaveBeenCalledWith("assetconvert:changed", data);
  });

  it("shows a success toast when convertedQty is positive", () => {
    renderHook(() => useAssetconvertEvent());
    const data = { convertId: 131236, convertedQty: 1 };

    act(() => mocks.onMessage?.(data));

    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "transfer.convert.completed",
    );
    expect(mocks.toastError).not.toHaveBeenCalled();
    expect(mocks.emit).toHaveBeenCalledWith("assetconvert:changed", data);
  });

  it("does not show a result toast for an unsupported convertedQty", () => {
    renderHook(() => useAssetconvertEvent());
    const data = { convertId: 131237, convertedQty: -1 };

    act(() => mocks.onMessage?.(data));

    expect(mocks.toastError).not.toHaveBeenCalled();
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
    expect(mocks.emit).toHaveBeenCalledWith("assetconvert:changed", data);
  });
});
