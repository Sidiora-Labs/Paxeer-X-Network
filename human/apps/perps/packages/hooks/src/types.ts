import { API } from "@paxeer-network/types";
import { Portfolio } from "./paxeer-network/appStore";

// export type ImmerStateCreator<T> = StateCreator<CommonState, [["zustand/immer", never], never], [], T>;

export interface CalculatorCtx {
  //  positions:
  accountInfo?: API.AccountInfo;
  symbolsInfo?: Record<string, API.SymbolExt>;
  fundingRates?: Record<string, API.FundingRate>;
  // markPrices: Record<string, number> | null;
  // holding: API.Holding[];
  // positions: API.PositionTPSLExt[];
  // markets: Record<string, API.MarketInfoExt> | null;
  portfolio?: Portfolio;
  tokensInfo?: API.Token[];

  get: <T extends any>(fn: (output: Record<string, any>) => T) => T;
  outputToValue: () => any;

  get isReady(): boolean;

  // onComplete: (name: string, data: any) => void;
  saveOutput: (name: string, data: any) => void;

  // get positions(): API.PositionTPSLExt[];

  // onQueueComplete: () => void;

  clearCache: () => void;
}

export enum CalculatorScope {
  MARK_PRICE = "markPrice",
  INDEX_PRICE = "indexPrice",
  POSITION = "position",
  ORDER = "order",
  TICK_PRICE = "tickPrice",
  ORDER_BOOK = "orderBook",
  PORTFOLIO = "portfolio",
  MARKET = "market",
}

export interface Calculator<T = any> {
  name: string;
  calc: (scope: CalculatorScope, data: any, ctx: CalculatorCtx) => T | null;
  cache: (result: T) => void;
  update: (data: T, scope: CalculatorScope) => void;

  destroy?: () => void;

  /**
   * Optional method to process a single data item from an array.
   * If implemented, the scheduler will flatten array data and process items individually
   * for better time slicing granularity.
   *
   * @param scope - The calculation scope
   * @param item - A single item from the data array
   * @param index - The index of the item in the original array
   * @param data - The original full data (for context)
   * @param ctx - The calculator context
   * @returns The processed item result, or null to skip
   */
  calcItem?: (
    scope: CalculatorScope,
    item: any,
    index: number,
    data: any,
    ctx: CalculatorCtx,
  ) => any;

  /**
   * Optional method to aggregate individual item results back into the final result.
   * Called after all items are processed if calcItem is used.
   *
   * @param scope - The calculation scope
   * @param items - Array of processed items
   * @param originalData - The original input data
   * @param ctx - The calculator context
   * @returns The aggregated result
   */
  aggregateItems?: (
    scope: CalculatorScope,
    items: any[],
    originalData: any,
    ctx: CalculatorCtx,
  ) => T | null;

  /**
   * Optional method to determine if data should be flattened for this calculator.
   * If not provided, defaults to checking if data is an array with length > LARGE_ARRAY_THRESHOLD.
   *
   * @param scope - The calculation scope
   * @param data - The input data
   * @returns true if data should be flattened, false otherwise
   */
  shouldFlatten?: (scope: CalculatorScope, data: any) => boolean;
}

export interface CalculatorScheduler {
  calc: (
    scope: CalculatorScope,
    calculators: Calculator[],
    data: any,
    ctx: CalculatorCtx,
  ) => Promise<any>;
  update: (scope: CalculatorScope, calculators: Calculator[], data: any) => any;
}
