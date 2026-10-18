import { MarginMode, OrderStatus } from "@paxeer-network/types";
import type { TradingViewColorConfig } from "@paxeer-network/ui";
import type { Order } from "./charting_library";

export type {
  IBrokerConnectionAdapterHost,
  IChartingLibraryWidget,
  IExecutionLineAdapter,
  InstrumentInfo,
  PlaceOrderResult,
  PlacedOrder,
  PreOrder,
  LibrarySymbolInfo,
  QuotesCallback,
  ResolutionString,
  SubscribeBarsCallback,
  QuoteData,
  IOrderLineAdapter,
  TradeContext,
  TradingTerminalWidgetOptions,
  IBrokerTerminal,
  IBrokerWithoutRealtime,
  LanguageCode,
  DatafeedConfiguration,
  ErrorCallback,
  HistoryCallback,
  OnReadyCallback,
  ResolveCallback,
  SearchSymbolResultItem,
  SearchSymbolsCallback,
  SymbolResolveExtension,
  Bar,
  HistoryMetadata,
  PeriodParams,
  VisiblePlotsSet,
  OrderType as ChartOrderType,
} from "./charting_library";

export type ChartOrder = Order & {
  rootAlgoId?: number;
  parentAlgoId?: number;
  areBothPTPAndPSLActivated?: boolean;
};

export enum OrderType {
  Limit = 1,
  Market = 2,
  Stop = 3,
  StopLimit = 4,
  OCO = 5,
  TrailingStop = 6,
  PostOnly = 7,
  Ask = 8,
  Bid = 9,
  POSITION_TP = 10,
  POSITION_SL = 11,
  BRACKET_TP = 12,
  BRACKET_SL = 13,
  BRACKET = 14,
}

export enum Side {
  Buy = 1,
  Sell = -1,
}

export enum ParentType {
  Order = 1,
  Position = 2,
}

export enum StopType {
  StopLoss = 0,
  TrailingStop = 1,
}

export enum ChartMode {
  BASIC = 0,
  ADVANCED = 1,
  UNLIMITED = 2,
  MOBILE = 3,
}

export enum PositionSide {
  LONG = "LONG",
  SHORT = "SHORT",
  BOTH = "BOTH",
}

export type ChartPosition = {
  symbol: string;
  open: number;
  balance: number;
  leverage?: number;
  closablePosition: number;
  unrealPnl: number;
  interest: number;
  unrealPnlDecimal: number;
  basePriceDecimal: number;
  positionSide?: PositionSide;
  /** Liquidation price from position (est_liq_price); used for chart liquidation line and 3s fallback. */
  est_liq_price?: number | null;
  marginMode?: MarginMode;
  /** Current mark price for TPSL type determination (target vs. mark). */
  markPrice?: number;
};

export enum SideType {
  BUY = "BUY",
  SELL = "SELL",
}

export enum AlgoType {
  TAKE_PROFIT = "TAKE_PROFIT",
  STOP_LOSS = "STOP_LOSS",
  OCO = "OCO",
  TRAILING_STOP = "TRAILING_STOP",
  BRACKET = "BRACKET",
  STOP_BRACKET = "STOP_BRACKET",
  STOP = "STOP", // create only
  POSITIONAL_TP_SL = "POSITIONAL_TP_SL", // create only
  TP_SL = "TP_SL", // create only
  BRACKET_TP_SL = "BRACKET_TP_SL",
  STOP_BRACKET_TP_SL = "STOP_BRACKET_TP_SL",
}

export enum OrderType {
  LIMIT = "LIMIT",
  MARKET = "MARKET",
  IOC = "IOC",
  POST_ONLY = "POST_ONLY",
  FOK = "FOK",
  LIQUIDATE = "LIQUIDATE", // backend create only
  ASK = "ASK",
  BID = "BID",
  CLOSE_POSITION = "CLOSE_POSITION", // create childOrders only
  AC = "AC",
  ADL_CLOSE = "ADL_CLOSE",
  BLP_ASSIGNEE = "BLP_ASSIGNEE",
}

export enum OrderCombinationType {
  LIMIT = "LIMIT",
  MARKET = "MARKET",
  STOP_LIMIT = "STOP_LIMIT",
  STOP_MARKET = "STOP_MARKET",
  OCO = "OCO",
  OCO_LIMIT = "OCO_LIMIT",
  OCO_MARKET = "OCO_MARKET",
  ASK = "ASK",
  BID = "BID",
  POST_ONLY = "POST_ONLY",
  IOC = "IOC",
  FOK = "FOK",
  LIQUIDATE = "LIQUIDATE",
  SETTLEMENT = "SETTLEMENT",
  TRAILING_STOP = "TRAILING_STOP",
  POSITIONAL_TP_SL = "POSITIONAL_TP_SL",
  TP_SL = "TP_SL",
  BRACKET = "BRACKET",
  BRACKET_LIMIT = "BRACKET_LIMIT",
  BRACKET_MARKET = "BRACKET_MARKET",
  STOP_BRACKET_LIMIT = "STOP_BRACKET_LIMIT",
  STOP_BRACKET_MARKET = "STOP_BRACKET_MARKET",
  AC = "AC",
  ADL_CLOSE = "ADL_CLOSE",
  BLP_ASSIGNEE = "BLP_ASSIGNEE",
}

export enum OrderTagType {
  DEFAULT = "default",
  LOSS_AUTOSETTLE = "LOSS_AUTOSETTLE",
}

export enum TriggerPriceType {
  LAST_PRICE = "MARKET_PRICE", // API Takes MARKET_PRICE
  MARK_PRICE = "MARK_PRICE",
}

export enum TriggerStatusType {
  USELESS = "USELESS",
  TRIGGERING = "TRIGGERING",
  SUCCESS = "SUCCESS",
  FAILED = "FAILED",
}

type OrderOverview = {
  status: OrderStatus; // algo is rootAlgoStatus or ordinary is status
  quantity: number; // oco by triggerStatus， if triggered is child quantity，otherwise is order quantity
  executed: number;
};
export interface OrderInterface {
  parent_algo_type?: string;
  margin_mode?: MarginMode;
  order_id: number;
  algo_order_id: number; // Algo order root id (also the algo order first parent_order_id)
  parent_algo_order_id?: number;
  root_algo_order_id?: number;
  root_algo_status: OrderStatus; // algo order only, is order overview status
  symbol: string;
  order_tag: OrderTagType;
  algo_type: AlgoType;
  side: SideType;
  position_side?: PositionSide;
  quantity: number;
  is_triggered: boolean;
  trigger_price: number; // trailing stop represents the activated_price
  trigger_price_type: TriggerPriceType;
  trigger_status: TriggerStatusType;
  type: OrderType;
  status: OrderStatus;
  algo_status: OrderStatus;
  price: number; // if oco, 1st leg
  executed: number; // total executed quantity
  avg_exec_price: number;
  reduce_only: boolean;
  create_time: string;
  updated_time: string;
  visible_quantity?: number;
  is_activated: boolean;
  callback_rate?: number;
  callback_value?: number;
  extreme_price?: number;
  activated_price?: number;
  realized_pnl?: number;
  child_orders?: OrderInterface[];
  overview: OrderOverview;
  // format data
  visible?: boolean;
  total?: number;
  // ws data
  executed_quantity?: number; // Only for the current push
  root_algo_order_type?: OrderType;
  root_algo_order_algo_type?: AlgoType;
  root_algo_order_qty?: number;
  root_algo_order_price?: number;
  total_fee?: number;
  fee_asset?: string;
  position_qty?: number;
  average_executed_price?: number;
  total_executed_quantity?: number;
}

export interface ColorConfigInterface extends TradingViewColorConfig {}
