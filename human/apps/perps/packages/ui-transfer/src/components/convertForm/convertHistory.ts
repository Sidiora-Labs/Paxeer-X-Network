import { Decimal } from "@paxeer-network/utils";

export type ConvertHistoryStatus =
  | "completed"
  | "succeeded"
  | "pending"
  | "failed"
  | "cancelled";

export type ConvertProgressStatus =
  | ConvertHistoryStatus
  | "checking"
  | "delayed";

export interface ConvertHistoryTransaction {
  converted_asset: string;
  converted_qty: number;
  received_asset: string;
  received_qty: number;
  result?: string;
}

export interface ConvertHistoryRecord {
  convert_id: number;
  converted_asset?: Record<string, number> | null;
  received_asset: string;
  received_qty: number;
  type: "auto" | "manual";
  status: ConvertHistoryStatus;
  created_time: number;
  details: ConvertHistoryTransaction[];
}

export interface ConvertHistoryResponse {
  rows: ConvertHistoryRecord[];
  meta: {
    total: number;
    records_per_page: number;
    current_page: number;
  };
}

export interface TrackedConvertRequest {
  accountId?: string;
  convertId?: number;
  previousMaxConvertId: number;
  sourceToken: string;
  sourceAmount: string;
  targetToken: string;
  targetAmount: string;
}

export type ConvertRequestOrigin = "submitted" | "recovered";

export const getTrackedConvertRequestForAccount = (
  request: TrackedConvertRequest | undefined,
  accountId: string | undefined,
) => (request?.accountId === accountId ? request : undefined);

export const isValidConvertAmount = (value: unknown) => {
  const amount = Number(value);
  return Number.isFinite(amount) && amount > 0;
};

export const getConvertTargetAmount = (submittedAmount: string | undefined) =>
  isValidConvertAmount(submittedAmount) ? String(submittedAmount) : "-";

export const getConvertReceivedAmount = (record?: ConvertHistoryRecord) => {
  if (
    !record ||
    (record.status !== "completed" && record.status !== "succeeded")
  ) {
    return "-";
  }

  if (isValidConvertAmount(record.received_qty)) {
    return String(record.received_qty);
  }

  const detailAmount = record.details.reduce((total, detail) => {
    if (
      detail.result?.toLowerCase() !== "succeeded" ||
      !isValidConvertAmount(detail.received_qty)
    ) {
      return total;
    }

    return total.plus(detail.received_qty);
  }, new Decimal(0));

  return detailAmount.gt(0) ? detailAmount.toString() : "-";
};

export const createTrackedConvertRequest = (
  record: ConvertHistoryRecord,
): TrackedConvertRequest => {
  const sourceEntry = Object.entries(record.converted_asset ?? {})[0];
  const sourceDetail = record.details[0];

  return {
    convertId: record.convert_id,
    previousMaxConvertId: record.convert_id - 1,
    sourceToken: sourceEntry?.[0] || sourceDetail?.converted_asset || "-",
    sourceAmount: String(
      sourceEntry?.[1] ?? sourceDetail?.converted_qty ?? "-",
    ),
    targetToken: record.received_asset || "USDC",
    targetAmount: getConvertTargetAmount(undefined),
  };
};

export const getEffectiveConvertStatus = (
  record: Pick<ConvertHistoryRecord, "status" | "details">,
): ConvertHistoryStatus => {
  if (record.status === "pending") {
    return "pending";
  }

  const hasFailedTransaction = record.details.some(
    (detail) => detail.result?.toLowerCase() === "failed",
  );
  if (hasFailedTransaction) {
    return "failed";
  }

  const hasOnlySucceededTransactions =
    record.status === "completed" &&
    record.details.length > 0 &&
    record.details.every(
      (detail) => detail.result?.toLowerCase() === "succeeded",
    );

  return hasOnlySucceededTransactions ? "succeeded" : record.status;
};

export const getConvertProgressStatus = (
  status: ConvertHistoryStatus | undefined,
  isRefreshing: boolean,
  isDelayed: boolean,
): ConvertProgressStatus | undefined => {
  if (status !== "pending") {
    return status;
  }
  if (isRefreshing) {
    return "checking";
  }
  return isDelayed ? "delayed" : "pending";
};

export const shouldDisplayConvertProgress = (
  status: ConvertHistoryStatus | undefined,
  isRecoveredRequest: boolean,
) => Boolean(status && (!isRecoveredRequest || status === "pending"));

const recordContainsToken = (record: ConvertHistoryRecord, token: string) => {
  return (
    Object.keys(record.converted_asset ?? {}).some(
      (asset) => asset.toLowerCase() === token.toLowerCase(),
    ) ||
    record.details.some(
      (detail) => detail.converted_asset?.toLowerCase() === token.toLowerCase(),
    )
  );
};

export const findTrackedConvertRecord = (
  rows: ConvertHistoryRecord[],
  request: TrackedConvertRequest,
) => {
  if (request.convertId !== undefined) {
    return rows.find((record) => record.convert_id === request.convertId);
  }

  return rows.find(
    (record) =>
      record.convert_id > request.previousMaxConvertId &&
      record.type === "manual" &&
      recordContainsToken(record, request.sourceToken),
  );
};

/** Latest pending convert for the account. Forward and reverse share this slot. */
export const findLatestPendingConvertRecord = (rows: ConvertHistoryRecord[]) =>
  rows.find((record) => getEffectiveConvertStatus(record) === "pending");

export const getConvertIdFromResponse = (response: unknown) => {
  if (!response || typeof response !== "object") {
    return undefined;
  }

  const value = response as {
    converted_id?: unknown;
    convertId?: unknown;
    convert_id?: unknown;
    data?: {
      converted_id?: unknown;
      convertId?: unknown;
      convert_id?: unknown;
    };
  };
  const convertId =
    value.data?.converted_id ??
    value.converted_id ??
    value.data?.convertId ??
    value.data?.convert_id ??
    value.convertId ??
    value.convert_id;

  const numericConvertId = Number(convertId);
  return Number.isFinite(numericConvertId) ? numericConvertId : undefined;
};

export const isConvertEventForRequest = (
  event: unknown,
  request: TrackedConvertRequest,
) => {
  const eventConvertId = getConvertIdFromResponse(event);
  if (eventConvertId === undefined) {
    return false;
  }

  return request.convertId !== undefined
    ? eventConvertId === request.convertId
    : eventConvertId > request.previousMaxConvertId;
};

export const normalizeConvertSlippage = (value: number) => {
  return Number.isFinite(value) ? Math.max(0.2, value) : 0.5;
};
