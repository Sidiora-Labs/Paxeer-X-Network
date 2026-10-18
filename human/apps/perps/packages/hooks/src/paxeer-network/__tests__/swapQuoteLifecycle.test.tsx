import React from "react";
import { createRoot } from "react-dom/client";
import { act } from "react-dom/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { useSwapQuote } from "../useSwapQuote";

const mock = vi.hoisted(() => ({ trigger: vi.fn(), reset: vi.fn() }));
vi.mock("../../useMutation", () => ({
  useMutation: () => [mock.trigger, { reset: mock.reset, isMutating: false }],
}));
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
let root: ReturnType<typeof createRoot>;
let state: ReturnType<typeof useSwapQuote>;
afterEach(() => {
  if (root) act(() => root.unmount());
  vi.clearAllMocks();
});
const request = {
  fromToken: "USDC",
  toToken: "USDG",
  amount: 100,
  slippage: 0.005,
};
const quote = (amount: string) => ({
  success: true,
  data: {
    pathId: amount,
    traceId: "trace",
    chainId: "",
    fromToken: { tokenAddress: "USDC", amount: "100000000", value: "100" },
    toToken: { tokenAddress: "USDG", estimatedAmount: amount },
    valueCurrency: "USD",
    netOutValue: "99",
    priceImpactPercent: null,
    slippageLimitPercent: "0",
    expiresAt: Date.now() + 10000,
    gasEstimate: {
      gasUnits: "0",
      gasPriceWei: null,
      nativeTokenSymbol: "",
      estimatedFeeAmount: null,
      estimatedFeeValue: null,
    },
  },
});
function mount() {
  root = createRoot(document.createElement("div"));
  const Harness = () => {
    state = useSwapQuote();
    return null;
  };
  act(() => root.render(<Harness />));
}
it("keeps the newest quote when responses arrive out of order", async () => {
  let first: (value: unknown) => void = () => {};
  let second: (value: unknown) => void = () => {};
  mock.trigger
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          first = resolve;
        }),
    )
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          second = resolve;
        }),
    );
  mount();
  let p1: any, p2: any;
  act(() => {
    p1 = state[0](request);
    p2 = state[0]({ ...request, amount: 200 });
  });
  await act(async () => {
    second(quote("198000000"));
    await p2;
  });
  await act(async () => {
    first(quote("99000000"));
    await p1;
  });
  expect(state[1].request?.amount).toBe(200);
  expect(state[1].data?.toToken.estimatedAmount).toBe("198000000");
  expect(mock.trigger).toHaveBeenCalledWith(request);
});
it("reset invalidates a pending response", async () => {
  let resolve: (value: unknown) => void = () => {};
  mock.trigger.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  mount();
  let pending: any;
  act(() => {
    pending = state[0](request);
    state[1].reset();
  });
  await act(async () => {
    resolve(quote("99000000"));
    await pending;
  });
  expect(state[1].data).toBeUndefined();
});
