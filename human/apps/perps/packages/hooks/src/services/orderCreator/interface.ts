import { API, OrderType, PaxeerNetworkOrder } from "@paxeer-network/types";

export type OrderValidationItem =
  | {
      type: "required";
      message: string;
      value?: never;
    }
  | {
      type: "max" | "min";
      message: string;
      value: number | string;
    }
  | {
      type: "range";
      message: string;
      value?: never;
      min: number | string;
      max: number | string;
    }
  | {
      type: "priceErrorMin" | "priceErrorMax";
      message: string;
      value?: never;
    }
  | {
      type: number;
      message: string;
      value?: never;
    };

export type OrderValidationResult = {
  [P in keyof PaxeerNetworkOrder]?: OrderValidationItem;
};

export type OrderFormEntity = Pick<
  PaxeerNetworkOrder,
  | "order_price"
  | "order_quantity"
  | "total"
  | "reduce_only"
  | "slippage"
  | "margin_mode"
>;

export type ValuesDepConfig = {
  // token: API.TokenInfo;
  symbol: API.SymbolExt;
  maxQty: number;
  markPrice: number;
  estSlippage?: number | null;
  /** [ask0, bid0] */
  askAndBid?: number[];
};

export interface OrderCreator<T> {
  create: (values: T, configs: ValuesDepConfig) => T;
  validate: (
    values: T,
    configs: ValuesDepConfig,
  ) => Promise<{
    [P in keyof T]?: OrderValidationItem;
  }>;

  get type(): OrderType;
}
