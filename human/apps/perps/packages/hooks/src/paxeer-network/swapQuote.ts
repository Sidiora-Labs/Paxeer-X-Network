export const SWAP_QUOTE_URL = "/v1/swap/quote";

export interface SwapQuoteRequest extends Record<string, string | number> {
  fromToken: string;
  toToken: string;
  amount: number;
  slippage: number;
}

export interface SwapQuoteToken {
  tokenAddress: string;
  amount?: string;
  value?: string;
  estimatedAmount?: string;
  estimatedValue?: string;
}

export interface GasEstimate {
  gasUnits: string;
  gasPriceWei: string | null;
  nativeTokenSymbol: string;
  estimatedFeeAmount: string | null;
  estimatedFeeValue: string | null;
}

export interface SwapQuoteData {
  pathId: string;
  traceId: string;
  chainId: string;
  fromToken: SwapQuoteToken & { amount: string; value: string };
  toToken: SwapQuoteToken;
  valueCurrency: string;
  netOutValue: string;
  priceImpactPercent: string | null;
  slippageLimitPercent: string;
  gasEstimate: GasEstimate;
  expiresAt: number;
}

export interface SwapQuoteResponse {
  success: boolean;
  data?: SwapQuoteData;
  code?: number | string;
  message?: string;
  timestamp?: number;
}

export interface SwapQuoteError extends Error {
  code?: number | string;
  timestamp?: number;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

const isString = (value: unknown): value is string => typeof value === "string";

const isNullableString = (value: unknown): value is string | null =>
  value === null || isString(value);

const isOptionalString = (value: unknown): value is string | undefined =>
  value === undefined || isString(value);

const invalidQuoteResponseError = () => {
  const error = new Error("Invalid quote response") as SwapQuoteError;
  error.code = "INVALID_QUOTE_RESPONSE";
  error.timestamp = Date.now();
  return error;
};

export const isSwapQuoteData = (value: unknown): value is SwapQuoteData => {
  if (!isRecord(value)) {
    return false;
  }

  const fromToken = value.fromToken;
  const toToken = value.toToken;
  const gasEstimate = value.gasEstimate;

  return (
    isString(value.pathId) &&
    isString(value.traceId) &&
    isString(value.chainId) &&
    isRecord(fromToken) &&
    isString(fromToken.tokenAddress) &&
    isString(fromToken.amount) &&
    isString(fromToken.value) &&
    isRecord(toToken) &&
    isString(toToken.tokenAddress) &&
    isOptionalString(toToken.estimatedAmount) &&
    isOptionalString(toToken.estimatedValue) &&
    value.valueCurrency === "USD" &&
    isString(value.netOutValue) &&
    isString(value.slippageLimitPercent) &&
    (value.priceImpactPercent === null || isString(value.priceImpactPercent)) &&
    isRecord(gasEstimate) &&
    isString(gasEstimate.gasUnits) &&
    isString(gasEstimate.nativeTokenSymbol) &&
    isNullableString(gasEstimate.gasPriceWei) &&
    isNullableString(gasEstimate.estimatedFeeAmount) &&
    isNullableString(gasEstimate.estimatedFeeValue) &&
    typeof value.expiresAt === "number" &&
    Number.isFinite(value.expiresAt) &&
    Number.isInteger(value.expiresAt)
  );
};

export const unwrapSwapQuoteResponse = (
  response: SwapQuoteResponse | SwapQuoteData | undefined,
) => {
  if (response && typeof response === "object" && "success" in response) {
    return response.success === false ? undefined : response.data;
  }

  return response as SwapQuoteData | undefined;
};

export const normalizeSwapQuoteResponse = (
  response: SwapQuoteResponse | SwapQuoteData,
) => {
  if (response && typeof response === "object" && "success" in response) {
    if (response.success === false) {
      const error = new Error(
        response.message || String(response.code || "Quote failed"),
      ) as SwapQuoteError;
      error.code = response.code;
      error.timestamp = response.timestamp;
      throw error;
    }

    if (!response.data) {
      throw invalidQuoteResponseError();
    }
  }

  const data = unwrapSwapQuoteResponse(response);
  if (data && !isSwapQuoteData(data)) {
    throw invalidQuoteResponseError();
  }

  return data;
};
