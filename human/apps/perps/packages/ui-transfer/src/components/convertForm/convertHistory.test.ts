import { describe, expect, it } from "vitest";
import {
  createTrackedConvertRequest,
  findTrackedConvertRecord,
  getConvertIdFromResponse,
  getConvertReceivedAmount,
  getConvertTargetAmount,
  getConvertProgressStatus,
  getEffectiveConvertStatus,
  getTrackedConvertRequestForAccount,
  isConvertEventForRequest,
  isValidConvertAmount,
  normalizeConvertSlippage,
  shouldDisplayConvertProgress,
} from "./convertHistory";
import type {
  ConvertHistoryRecord,
  TrackedConvertRequest,
} from "./convertHistory";

const record: ConvertHistoryRecord = {
  convert_id: 131211,
  converted_asset: {},
  received_asset: "USDC",
  received_qty: 0,
  type: "manual",
  status: "completed",
  created_time: 1787064481550,
  details: [
    {
      converted_asset: "ETH",
      received_asset: "USDC",
      converted_qty: 0,
      received_qty: -0.002944,
      result: "failed",
    },
  ],
};

const secondFailedRecord: ConvertHistoryRecord = {
  ...record,
  convert_id: 131210,
  details: [
    {
      ...record.details[0],
      received_qty: -0.00291,
    },
  ],
};

describe("convert history helpers", () => {
  it("treats a failed detail as the effective record status", () => {
    expect(getEffectiveConvertStatus(record)).toBe("failed");
    expect(getEffectiveConvertStatus(secondFailedRecord)).toBe("failed");
  });

  it.each(["completed", "pending", "failed", "cancelled"] as const)(
    "keeps the parent %s status when no detail failed",
    (status) => {
      expect(
        getEffectiveConvertStatus({
          ...record,
          status,
          details: [],
        }),
      ).toBe(status);
    },
  );

  it("returns failed when any detail failed", () => {
    expect(
      getEffectiveConvertStatus({
        ...record,
        details: [
          { ...record.details[0], result: "completed" },
          { ...record.details[0], result: "FAILED" },
        ],
      }),
    ).toBe("failed");
  });

  it("keeps the parent pending while a failed detail is still settling", () => {
    expect(
      getEffectiveConvertStatus({
        ...record,
        status: "pending",
      }),
    ).toBe("pending");
  });

  it("returns succeeded when a completed record only has succeeded details", () => {
    expect(
      getEffectiveConvertStatus({
        ...record,
        details: [
          { ...record.details[0], result: "succeeded" },
          { ...record.details[0], result: "SUCCEEDED" },
        ],
      }),
    ).toBe("succeeded");
  });

  it("keeps completed when details are empty or contain another result", () => {
    expect(getEffectiveConvertStatus({ ...record, details: [] })).toBe(
      "completed",
    );
    expect(
      getEffectiveConvertStatus({
        ...record,
        details: [{ ...record.details[0], result: "processing" }],
      }),
    ).toBe("completed");
  });

  it("matches a newly created manual record by token when no id is returned", () => {
    const request: TrackedConvertRequest = {
      previousMaxConvertId: 131210,
      sourceToken: "ETH",
      sourceAmount: "1",
      targetToken: "USDC",
      targetAmount: "2000",
    };

    expect(findTrackedConvertRecord([record], request)).toBe(record);
  });

  it.each([
    [{ data: { converted_id: 131211 } }, 131211],
    [{ convertId: 131211 }, 131211],
    [{ convert_id: "131211" }, 131211],
    [{ data: { convertId: 131211 } }, 131211],
    [{ data: { convert_id: "131211" } }, 131211],
  ])("reads the convert id from API and websocket payloads", (payload, id) => {
    expect(getConvertIdFromResponse(payload)).toBe(id);
  });

  it("matches a websocket event to the tracked convert request", () => {
    const request: TrackedConvertRequest = {
      convertId: 131211,
      previousMaxConvertId: 131210,
      sourceToken: "ETH",
      sourceAmount: "1",
      targetToken: "USDC",
      targetAmount: "2000",
    };

    expect(isConvertEventForRequest({ convertId: 131211 }, request)).toBe(true);
    expect(isConvertEventForRequest({ convertId: 131212 }, request)).toBe(
      false,
    );
  });

  it("matches a newer websocket event when the request API returned no id", () => {
    const request: TrackedConvertRequest = {
      previousMaxConvertId: 131210,
      sourceToken: "ETH",
      sourceAmount: "1",
      targetToken: "USDC",
      targetAmount: "2000",
    };

    expect(isConvertEventForRequest({ convertId: 131211 }, request)).toBe(true);
    expect(isConvertEventForRequest({ convertId: 131210 }, request)).toBe(
      false,
    );
  });

  it("does not match an older record", () => {
    const request: TrackedConvertRequest = {
      previousMaxConvertId: 131211,
      sourceToken: "ETH",
      sourceAmount: "1",
      targetToken: "USDC",
      targetAmount: "2000",
    };

    expect(findTrackedConvertRecord([record], request)).toBeUndefined();
  });

  it("only reuses a tracked request for its owning account", () => {
    const request: TrackedConvertRequest = {
      accountId: "account-1",
      previousMaxConvertId: 131210,
      sourceToken: "ETH",
      sourceAmount: "1",
      targetToken: "USDC",
      targetAmount: "2000",
    };

    expect(getTrackedConvertRequestForAccount(request, "account-1")).toBe(
      request,
    );
    expect(
      getTrackedConvertRequestForAccount(request, "account-2"),
    ).toBeUndefined();
  });

  it("creates a recovered request from a pending history record", () => {
    expect(
      createTrackedConvertRequest({
        ...record,
        converted_asset: { USDT: 0.5 },
        received_asset: "USDC",
        received_qty: 0,
        status: "pending",
        details: [],
      }),
    ).toEqual({
      convertId: 131211,
      previousMaxConvertId: 131210,
      sourceToken: "USDT",
      sourceAmount: "0.5",
      targetToken: "USDC",
      targetAmount: "-",
    });
  });

  it("keeps the submitted target amount when history is still pending", () => {
    expect(getConvertTargetAmount("0.499")).toBe("0.499");
  });

  it("does not use pending history amounts as the target estimate", () => {
    expect(getConvertTargetAmount(undefined)).toBe("-");
  });

  it("falls back to the sum of succeeded details when the completed total is empty", () => {
    expect(
      getConvertReceivedAmount({
        ...record,
        status: "completed",
        received_qty: 0,
        details: [
          { ...record.details[0], received_qty: 1, result: "failed" },
          {
            ...record.details[0],
            received_qty: 0.2,
            result: "succeeded",
          },
          {
            ...record.details[0],
            received_qty: 0.298,
            result: "succeeded",
          },
        ],
      }),
    ).toBe("0.498");
  });

  it("uses the completed history total", () => {
    expect(
      getConvertReceivedAmount({
        ...record,
        status: "completed",
        received_qty: 0.5,
        details: [
          { ...record.details[0], received_qty: 0.498, result: "succeeded" },
        ],
      }),
    ).toBe("0.5");
  });

  it("does not use pending or failed detail amounts as received amount", () => {
    expect(
      getConvertReceivedAmount({
        ...record,
        status: "pending",
        received_qty: 0.5,
        details: [
          { ...record.details[0], received_qty: 0.5, result: "succeeded" },
        ],
      }),
    ).toBe("-");
    expect(getConvertReceivedAmount(record)).toBe("-");
  });

  it("returns no received amount when completed history has no valid values", () => {
    expect(
      getConvertReceivedAmount({
        ...record,
        status: "completed",
        received_qty: 0,
        details: [
          { ...record.details[0], received_qty: 0, result: "succeeded" },
          { ...record.details[0], received_qty: -1, result: "failed" },
        ],
      }),
    ).toBe("-");
  });

  it.each([
    ["0.1", true],
    [1, true],
    ["", false],
    ["-", false],
    ["0", false],
    [-1, false],
    [Number.NaN, false],
    [Number.POSITIVE_INFINITY, false],
  ])("validates convert display amount %s as %s", (value, expected) => {
    expect(isValidConvertAmount(value)).toBe(expected);
  });

  it.each([
    [0.1, 0.2],
    [0.2, 0.2],
    [0.5, 0.5],
  ])("normalizes %s slippage to %s", (value, expected) => {
    expect(normalizeConvertSlippage(value)).toBe(expected);
  });

  it.each([
    ["pending", false, false, "pending"],
    ["pending", true, false, "checking"],
    ["pending", false, true, "delayed"],
    ["pending", true, true, "checking"],
    ["succeeded", true, true, "succeeded"],
    ["failed", true, true, "failed"],
  ] as const)(
    "maps %s with refreshing=%s and delayed=%s to %s",
    (status, isRefreshing, isDelayed, expected) => {
      expect(getConvertProgressStatus(status, isRefreshing, isDelayed)).toBe(
        expected,
      );
    },
  );

  it.each([
    [undefined, false, false],
    [undefined, true, false],
    ["pending", false, true],
    ["completed", false, true],
    ["succeeded", false, true],
    ["failed", false, true],
    ["cancelled", false, true],
    ["pending", true, true],
    ["completed", true, false],
    ["succeeded", true, false],
    ["failed", true, false],
    ["cancelled", true, false],
  ] as const)(
    "displays progress for status=%s with recovered=%s: %s",
    (status, isRecoveredRequest, expected) => {
      expect(shouldDisplayConvertProgress(status, isRecoveredRequest)).toBe(
        expected,
      );
    },
  );
});
