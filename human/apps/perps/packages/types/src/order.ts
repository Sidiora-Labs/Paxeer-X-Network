/**
 * Supported types for placing an order
 */
export enum OrderType {
  LIMIT = "LIMIT",
  MARKET = "MARKET",
  IOC = "IOC",
  FOK = "FOK",
  POST_ONLY = "POST_ONLY",
  ASK = "ASK",
  BID = "BID",
  STOP_LIMIT = "STOP_LIMIT",
  STOP_MARKET = "STOP_MARKET",
  /**
   * Only for POSITIONAL_TP_SL type algo order
   */
  CLOSE_POSITION = "CLOSE_POSITION",
  /**
   * Scaled order
   */
  SCALED = "SCALED",
  /**
   * trailing stop
   */
  TRAILING_STOP = "TRAILING_STOP",
}

/**
 * Margin mode for the order
 */
export enum MarginMode {
  ISOLATED = "ISOLATED",
  CROSS = "CROSS",
}

export enum BBOOrderType {
  COUNTERPARTY1 = "counterparty1",
  COUNTERPARTY5 = "counterparty5",
  QUEUE1 = "queue1",
  QUEUE5 = "queue5",
}

export enum OrderLevel {
  ONE = 0,
  TWO = 1,
  THREE = 2,
  FOUR = 3,
  FIVE = 4,
}

export enum AlgoOrderRootType {
  TP_SL = "TP_SL",
  POSITIONAL_TP_SL = "POSITIONAL_TP_SL",
  STOP = "STOP",
  BRACKET = "BRACKET",
  TRAILING_STOP = "TRAILING_STOP",
}

export enum TriggerPriceType {
  MARK_PRICE = "MARK_PRICE",
}
export enum PositionType {
  FULL = "FULL",
  PARTIAL = "PARTIAL",
}

export enum AlgoOrderType {
  TAKE_PROFIT = "TAKE_PROFIT",
  STOP_LOSS = "STOP_LOSS",
}

export enum OrderSide {
  BUY = "BUY",
  SELL = "SELL",
}

export enum PositionSide {
  LONG = "LONG",
  SHORT = "SHORT",
}

export enum OrderStatus {
  /** @deprecated */
  OPEN = "OPEN",
  NEW = "NEW",
  FILLED = "FILLED",
  PARTIAL_FILLED = "PARTIAL_FILLED",
  CANCELLED = "CANCELLED",
  REPLACED = "REPLACED",
  // CANCELLED + FILLED
  COMPLETED = "COMPLETED",
  //  NEW + PARTIAL_FILLED
  INCOMPLETE = "INCOMPLETE",
  REJECTED = "REJECTED",
}

export interface OrderExt {
  total: string;
}

export interface BaseOrder {
  symbol: string;
  order_type: OrderType;
  margin_mode: MarginMode;
  order_type_ext?: OrderType;
  order_price: string;
  order_quantity: string;
  order_amount?: number;
  visible_quantity: number;
  side: OrderSide;
  reduce_only: boolean;
  slippage: number;
  order_tag: string;
  level: OrderLevel;
  post_only_adjust: boolean;
  /** custom order id, it is used to identify the order from ws */
  client_order_id: string;
}

/** Scaled order fields */
export interface ScaledOrder {
  /** user-defined price at the first order (index 0) */
  start_price?: string;
  /** user-defined price at the last order (index total_orders - 1) */
  end_price?: string;
  /** total number of orders */
  total_orders?: string;
  /** quantity distribution type */
  distribution_type?: DistributionType;
  /** the ratio of qty[end] / qty[start] */
  skew?: string;
}

export enum TrailingCallbackType {
  VALUE = "value",
  RATE = "rate",
}

/**
 * Trailing Stop that allows users to set an activation price and a trailing amount (value / rate).
 * The order is only activated when the market reaches the activation price,
 * after which the trailing stop logic tracks the extreme price and triggers when the price retraces by the specified trailing amount.
 */
export interface TrailingStopOrder {
  /**
   * activated price
   */
  activated_price?: string;
  /**
   * i.e. the value = 100
   */
  callback_value?: string;
  /**
   * i.e. the value = 0.1 represent to 10%
   */
  callback_rate?: string;
}

export interface RegularOrder
  extends BaseOrder, OrderExt, ScaledOrder, TrailingStopOrder {
  // symbol:           string;
  // client_order_id:  string;
  // type:       OrderType;
  // price:      number;
  // quantity:   number;
}

export interface AlgoOrder extends BaseOrder, OrderExt {
  // symbol: string;
  quantity: string;
  type: OrderType;
  price: string;
  algo_type: AlgoOrderRootType;
  trigger_price_type: string;
  trigger_price: string;
  child_orders: AlgoOrderChildOrders[];
}

export interface BracketOrder extends AlgoOrder, OrderExt {
  /**
   * Computed take profit
   */
  position_type?: PositionType;
  // tp_enable?: boolean;
  // sl_enable?: boolean;
  tp_pnl?: string;
  tp_offset?: string;
  tp_offset_percentage?: string;
  /** Offset from mark price (UI / internal; same semantics as offset but base = mark). */
  tp_offset_from_mark?: string;
  tp_offset_percentage_from_mark?: string;
  tp_ROI?: string;
  tp_trigger_price?: string;
  tp_order_price?: string;
  tp_order_type?: OrderType;

  /**
   * Computed stop loss
   */
  sl_pnl?: string;
  sl_offset?: string;
  sl_offset_percentage?: string;
  sl_offset_from_mark?: string;
  sl_offset_percentage_from_mark?: string;
  sl_ROI?: string;
  sl_trigger_price?: string;
  sl_order_price?: string;
  sl_order_type?: OrderType;
}

export type PaxeerNetworkOrder = RegularOrder & AlgoOrder & BracketOrder;

export interface AlgoOrderChildOrders {
  symbol: string;
  algo_type: string;
  child_orders: ChildOrder[];
}

export interface ChildOrder {
  symbol: string;
  algo_type: AlgoOrderType;
  side: string;
  type: OrderType;
  trigger_price: string;
  price?: string;
  reduce_only: boolean;
  trigger_price_type?: string;
}

export interface TrailingStopOrder {
  activated_price?: string;
  callback_value?: string;
  callback_rate?: string;
}

export interface OrderEntity extends ScaledOrder, TrailingStopOrder {
  symbol: string;
  order_type: OrderType;
  margin_mode?: MarginMode;
  algo_type?: AlgoOrderRootType;
  order_type_ext?: OrderType;
  order_price?: string;
  order_quantity?: string;
  order_amount?: number;
  // Whether to display in the orderbook, default=order_quantity, not displayed when =0,
  visible_quantity?: number;
  reduce_only?: boolean;
  side: OrderSide;
  slippage?: number;

  // internal fields
  total?: string;
  // hideInOrderbook?: boolean;
  isStopOrder?: boolean;
  trigger_price?: string;
  order_tag?: string;
  client_order_id?: string;
  level?: OrderLevel;
}

export enum DistributionType {
  // enum value need to use lowercase to match the track params
  FLAT = "flat",
  ASCENDING = "ascending",
  DESCENDING = "descending",
  CUSTOM = "custom",
}

export type Optional<T, K extends keyof T> = Omit<T, K> & Partial<Pick<T, K>>;
export type RequireKeys<T extends object, K extends keyof T> = Required<
  Pick<T, K>
> &
  Partial<Omit<T, K>>;

export interface BaseAlgoOrderEntity<
  T extends AlgoOrderRootType,
> extends OrderEntity {
  algo_type: AlgoOrderRootType;
  child_orders: (Partial<Omit<AlgoOrderEntity<T>, "algo_type" | "type">> & {
    algo_type: AlgoOrderType;
    type: OrderType;
    child_orders?: BaseAlgoOrderEntity<T>["child_orders"];
    // trigger_price: number | string;
  })[];
  // if update the order, then need to provide the order_id
  algo_order_id?: number;
  client_order_id?: string;
  order_tag?: string;
  price?: number | string;
  quantity: number | string;
  reduce_only?: boolean;
  side: OrderSide;
  symbol: string;
  trigger_price: string;
  trigger_price_type: TriggerPriceType;
  type: OrderType;
  visible_quantity?: number;
  is_activated?: boolean;
  tp_trigger_price?: string | number;
  sl_trigger_price?: string | number;
  tp_order_price?: string | number;
  tp_order_type?: OrderType;
  sl_order_price?: string | number;
  sl_order_type?: OrderType;
  /** Mark-based TP/SL offsets (UI + API); mirrors `BracketOrder` when used on TP/SL algo forms. */
  tp_offset_from_mark?: string | number;
  tp_offset_percentage_from_mark?: string | number;
  sl_offset_from_mark?: string | number;
  sl_offset_percentage_from_mark?: string | number;
  // tp_enable?: boolean;
  // sl_enable?: boolean;
  position_type?: PositionType;
}

export type AlgoOrderEntity<
  T extends AlgoOrderRootType = AlgoOrderRootType.STOP,
> = T extends AlgoOrderRootType.TP_SL
  ? Optional<
      BaseAlgoOrderEntity<T>,
      "side" | "type" | "trigger_price" | "order_type"
    >
  : T extends AlgoOrderRootType.POSITIONAL_TP_SL
    ? Optional<
        BaseAlgoOrderEntity<T>,
        | "side"
        | "type"
        | "trigger_price"
        | "order_type"
        | "quantity"
        // | "tp_enable"
        // | "sl_enable"
        | "tp_order_price"
        | "tp_order_type"
        | "sl_order_price"
        | "sl_order_type"
        | "position_type"
      >
    : Omit<BaseAlgoOrderEntity<T>, "child_orders" | "order_type">;

export type TPSLOrderEntry = Optional<
  AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
  "side" | "type" | "trigger_price"
>;

export type BracketOrderEntry = Optional<
  AlgoOrderEntity<AlgoOrderRootType.BRACKET>,
  "side"
>;
