/* eslint-disable @typescript-eslint/no-namespace */
import { AlgoOrderRootType, MarginMode, OrderSide, OrderType } from "../order";

export enum AnnouncementType {
  Listing = "LISTING",
  Maintenance = "MAINTENANCE",
  Delisting = "DELISTING",
  Campaign = "CAMPAIGN",
  Vote = "VOTE",
}

export declare namespace API {
  /**
   * Symbol trading status returned by public symbol APIs.
   */
  export type SymbolTradingStatus =
    | "POST_ONLY"
    | "ACTIVE"
    | "REDUCE_ONLY"
    | "DELISTING";

  // /v1/public/auto_convert_threshold
  export interface ConvertThreshold {
    ltv_threshold: number;
    negative_usdc_threshold: number;
  }

  //v1/public/token
  export interface Token {
    token: string;
    token_hash: string;
    decimals: number;
    minimum_withdraw_amount: number;
    base_weight: number;
    discount_factor: number;
    haircut: number;
    user_max_qty: number;
    is_collateral: boolean;
    on_chain_swap: boolean;
    chain_details: ChainDetail[];
  }

  //v1/public/token
  export interface ChainDetail {
    chain_id: string;
    contract_address: string;
    decimals: number;
    exclusive_deposit_supported?: boolean;
  }

  // ws market, api v1/public/futures
  export interface MarketInfo {
    symbol: string;
    index_price: number;
    mark_price: number;
    sum_unitary_funding: number;
    est_funding_rate: number;
    last_funding_rate: number;
    next_funding_time: number;
    open_interest: string;
    "24h_open": number;
    "24h_close": number;
    "24h_high": number;
    "24h_low": number;
    /**
     * @deprecated
     * spelling mistake, use 24h_volume to instead, will be remove next version
     */
    "24h_volumn": number;
    "24h_volume": number;
    "24h_amount": number;
    /** Permissionless listing: display name e.g. BTC, without broker_id suffix */
    display_symbol_name?: string;
    /** Permissionless listing: broker id; null for non-community-listed symbols */
    broker_id?: string | null;
    /** Pre-TGE listing flag; controlled explicitly by broker/listing metadata. */
    is_pretge?: boolean;
    /** Trading status: POST_ONLY / ACTIVE / REDUCE_ONLY / DELISTING */
    status?: SymbolTradingStatus;
  }

  export interface MarketInfoExt extends MarketInfo {
    change: number;
    "24h_volume": number;
  }

  export interface AnnouncementRow {
    announcement_id: number | string;
    message: string;
    i18n?: Record<PropertyKey, string | null>;
    url?: string | null;
    type?: AnnouncementType | null;
    updated_time?: number | null;
    coverImage?: string;
  }

  export interface Announcement {
    last_updated_time?: number | null;
    rows?: AnnouncementRow[];
  }

  /**
   * v1/public/info
   */
  export interface Symbol {
    symbol: string;
    quote_min: number;
    quote_max: number;
    quote_tick: number;
    base_min: number;
    base_max: number;
    base_tick: number;
    min_notional: number;
    price_range: number;
    price_scope: number;
    std_liquidation_fee: number;
    liquidator_fee: number;
    claim_insurance_fund_discount: number;
    funding_period: number;
    cap_funding: number;
    floor_funding: number;
    interest_rate: number;
    created_time: number;
    updated_time: number;
    imr_factor: number;
    base_mmr: number;
    base_imr: number;
    /** Permissionless listing: display name e.g. BTC, without broker_id suffix */
    display_symbol_name?: string;
    /** Permissionless listing: broker id; null for non-community-listed symbols */
    broker_id?: string | null;
    /** Pre-TGE listing flag; controlled explicitly by broker/listing metadata. */
    is_pretge?: boolean;
    /** Trading status: POST_ONLY / ACTIVE / REDUCE_ONLY / DELISTING */
    status?: SymbolTradingStatus;
  }

  export interface TokenItem {
    token: string;
    token_hash: string;
    decimals: number;
    minimum_withdraw_amount: number;
    chain_details: ChainDetail[];
  }

  export interface ChainDetail {
    chain_id: string;
    contract_address: string;
    decimals: number;
    withdrawal_fee: number;
  }

  export interface SymbolExt extends Symbol {
    base: string;
    base_dp: number;

    quote: string;
    quote_dp: number;
    type: string;
    name: string;
  }

  export interface RwaSymbol {
    symbol: string;
    status: "open" | "close";
    next_open: number;
    next_close: number;
    market_session?:
      | "US_STOCK"
      | "US_STOCK_EXT"
      | "HK_STOCK"
      | "KR_STOCK"
      | "CN_STOCK"
      | "FX"
      | string;

    base: string;
    quote: string;
    type: string;
    name: string;
  }

  export interface Order {
    symbol: string;
    status: string;
    side: string;
    order_id: number;
    algo_order_id?: number;
    user_id: number;
    price: number | null;
    type: string;
    quantity: number;
    amount: null;
    visible: number;
    executed: number;
    total_fee: number;
    fee_asset: string;
    client_order_id?: string;
    average_executed_price: number;
    total_executed_quantity: number;
    visible_quantity: number;
    created_time: number;
    updated_time: number;
    reduce_only: boolean;
    trigger_price?: number;
    order_tag?: string;
    margin_mode?: MarginMode;
  }

  export interface OrderExt extends Order {
    mark_price: string;
  }

  export interface AlgoOrder {
    algo_order_id: number;
    root_algo_order_id: number;
    parent_algo_order_id: number;
    parent_algo_type: AlgoOrderRootType;
    symbol: string;
    algo_type: string;
    child_orders: AlgoOrder[];
    side: string;
    quantity: number;
    is_triggered: boolean;
    is_activated: boolean;
    trigger_price: number;
    trigger_price_type: string;
    type: OrderType;
    root_algo_status: string;
    algo_status: string;
    price?: number;
    total_executed_quantity: number;
    visible_quantity: number;
    total_fee: number;
    fee_asset: string;
    reduce_only: boolean;
    created_time: number;
    updated_time: number;
    order_tag?: string;
    client_order_id?: string;

    // trailing stop order
    activated_price?: number;
    callback_value?: number;
    callback_rate?: number;
    extreme_price?: number;
    margin_mode?: MarginMode;
  }

  export interface AlgoOrderExt extends AlgoOrder {
    mark_price: string;
    position?: Partial<Position>;
    tp_trigger_price?: number;
    sl_trigger_price?: number;
  }

  export interface OrderResponse {
    rows: (Order | AlgoOrder)[];
    meta: {
      total: number;
      current_page: number;
      records_per_page: number;
    };
  }

  export interface FundingRate {
    symbol: string;
    est_funding_rate: number;
    est_funding_rate_timestamp: number;
    last_funding_rate: number;
    last_funding_rate_timestamp: number;
    next_funding_time: number;
    sum_unitary_funding: number;
  }

  export interface FundingPeriodData {
    rate: number;
    positive: number;
    negative: number;
  }

  export interface FundingDetails {
    symbol: string;
    quote_min: number;
    quote_max: number;
    quote_tick: number;
    base_min: number;
    base_max: number;
    base_tick: number;
    min_notional: number;
    price_range: number;
    price_scope: number;
    std_liquidation_fee: number;
    liquidator_fee: number;
    claim_insurance_fund_discount: number;
    funding_period: number;
    cap_funding: number;
    floor_funding: number;
    interest_rate: number;
    created_time: number;
    updated_time: number;
    liquidation_tier: number;
    cap_ir: number;
    floor_ir: number;
    mark_index_price_deviation_floor: number;
    mark_index_price_deviation_cap: number;
    global_max_oi_cap: number;
    base_mmr: number;
    base_imr: number;
    imr_factor: number;
    deviation_factor: number;
    /** Permissionless listing: display name e.g. BTC, without broker_id suffix */
    display_symbol_name?: string;
    /** Permissionless listing: broker id; null for non-community-listed symbols */
    broker_id?: string | null;
  }

  export interface FundingHistory {
    symbol: string;
    data_start_time: string;
    funding: {
      last: FundingPeriodData;
      "1d": FundingPeriodData;
      "3d": FundingPeriodData;
      "7d": FundingPeriodData;
      "14d": FundingPeriodData;
      "30d": FundingPeriodData;
      "90d": FundingPeriodData;
    };
  }

  export interface PositionInfo extends PositionAggregated {
    // margin_ratio: number;
    // initial_margin_ratio: number;
    // maintenance_margin_ratio: number;
    // open_margin_ratio: number;
    // current_margin_ratio_with_orders: number;
    // initial_margin_ratio_with_orders: number;
    // maintenance_margin_ratio_with_orders: number;
    // total_collateral_value: number;
    // free_collateral: number;
    rows: Position[];
    // total_pnl_24_h: number;
  }

  export interface PositionAggregated {
    margin_ratio: number;
    initial_margin_ratio: number;
    maintenance_margin_ratio: number;
    open_margin_ratio: number;
    current_margin_ratio_with_orders: number;
    initial_margin_ratio_with_orders: number;
    maintenance_margin_ratio_with_orders: number;
    total_collateral_value: number;
    free_collateral: number;
    total_pnl_24_h: number;
    /**
     * @deprecated use total_unreal_pnl instead
     */
    unrealPnL: number;
    total_unreal_pnl: number;
    total_unreal_pnl_index?: number;
    /**
     * @deprecated use total_unsettled_pnl instead
     */
    unsettledPnL: number;
    total_unsettled_pnl: number;
    notional: number;
    unrealPnlROI: number;
    unrealPnlROI_index?: number;
    total_unsettled_cross_pnl?: number;
    total_unsettled_isolated_pnl?: number;
  }

  export interface Position {
    account_id?: string;
    symbol: string;
    position_qty: number;
    cost_position: number;
    last_sum_unitary_funding: number;
    /**
     * Accumulated funding fee since the position was opened, returned by /v1/positions
     */
    accrued_funding_fee?: number;
    pending_long_qty: number;
    pending_short_qty: number;
    settle_price: number;
    average_open_price: number;
    unrealized_pnl: number;
    unrealized_pnl_index?: number;
    unrealized_pnl_ROI: number;
    unsettled_pnl: number;
    unsettled_pnl_ROI: number;
    unrealized_pnl_ROI_index?: number;
    mark_price: number;
    index_price?: number;
    est_liq_price: number | null;
    timestamp: number;
    /**
     * Maintenance margin ratio
     */
    mmr: number;
    imr: number;
    IMR_withdraw_orders: number;
    MMR_with_orders: number;
    pnl_24_h: number;
    fee_24_h: number;
    fundingFee?: number;
    leverage: number;
    margin?: number;
    margin_mode?: MarginMode;
  }

  export interface PositionExt extends Position {
    notional: number;
    mm: number;
  }

  export interface PositionTPSLExt extends PositionExt {
    full_tp_sl?: {
      tp_trigger_price?: number;
      sl_trigger_price?: number;
      algo_order?: AlgoOrder;
    };
    partial_tp_sl?: {
      tp_trigger_price?: number;
      sl_trigger_price?: number;
      order_num?: number;
      algo_order?: AlgoOrder;
    };

    // has_position_tp_sl: boolean;

    /**
     * related position tp/sl order
     */
    algo_order?: AlgoOrder;
  }

  export interface PositionsTPSLExt extends PositionAggregated {
    rows: PositionTPSLExt[];
  }

  export interface Trade {
    symbol: Symbol;
    side: OrderSide;
    ts: number;
    executed_price: number;
    executed_quantity: number;
    executed_timestamp: number;
  }

  export interface Holding {
    token: string;
    holding: number;
    frozen: number;
    pending_short: number;
    isolated_margin: number;
    isolated_order_frozen: number;
    updated_time: number;
  }

  export interface AccountInfo {
    account_id: string;
    email: string;
    account_mode: string;
    tier: string;
    futures_tier: string;
    maintenance_cancel_orders: boolean;
    taker_fee_rate: number;
    maker_fee_rate: number;
    rwa_taker_fee_rate?: number;
    rwa_maker_fee_rate?: number;
    max_leverage: number;
    futures_taker_fee_rate: number;
    futures_maker_fee_rate: number;
    imr_factor: { [key: string]: number };
    max_notional: { [key: string]: number };
  }

  export interface Chain {
    dexs: string[];
    network_infos: NetworkInfos;
    token_infos: TokenInfo[];
    nativeToken?: TokenInfo;
    address?: string;
    symbol?: string;
    on_chain_swap?: boolean;
    // nativeToken
  }

  export interface NetworkInfos {
    name: string;
    shortName: string;
    public_rpc_url: string;
    chain_id: number;
    currency_symbol: string;
    bridge_enable: boolean;
    mainnet: boolean;
    est_txn_mins: number | null;
    explorer_base_url: string;
    bridgeless?: boolean;
    withdrawal_fee?: number;
    minimum_withdraw_amount?: number;
    vault_address: string;
    currency_decimal?: number;

    cross_chain_router: string;
    depositor: string;
  }

  export interface TokenInfo {
    address?: string;
    base_weight: number;
    decimals?: number;
    /** token decimals */
    token_decimal?: number;
    discount_factor?: number | null;
    display_name?: string;
    haircut: number;
    is_collateral: boolean;
    symbol?: string;
    user_max_qty: number;
    precision?: number;
    minimum_withdraw_amount: number;
    swap_enable?: boolean;
  }

  export interface Chain {
    token: string;
    token_hash: string;
    decimals: number;
    minimum_withdraw_amount: number;
    base_weight: number;
    discount_factor?: number | null;
    haircut: number;
    user_max_qty: number;
    is_collateral: boolean;
    chain_details: ChainDetail[];
  }

  // export interface Token{

  // }

  export interface ChainDetail {
    chain_id: string;
    chain_name?: string;
    contract_address: string;
    decimals: number;
    withdrawal_fee: number;
    cross_chain_withdrawal_fee: number;
    display_name: string;
  }

  export interface AssetHistory {
    meta: RecordsMeta;
    rows: AssetHistoryRow[];
  }

  export interface RecordsMeta {
    total: number;
    records_per_page: number;
    current_page: number;
  }

  export interface AssetHistoryRow {
    id: string;
    tx_id: string;
    side: string;
    token: string;
    amount: number;
    fee: number;
    trans_status: string;
    created_time: number;
    updated_time: number;
    chain_id: string;
  }

  export interface FundingFeeHistory {
    meta: RecordsMeta;
    rows: FundingFeeRow[];
  }

  export interface FundingFeeRow {
    symbol: string;
    margin_mode: MarginMode;
    funding_rate: number;
    mark_price: number;
    funding_fee: number;
    payment_type: string;
    status: string;
    created_time: number;
    updated_time: number;
  }

  export interface StrategyVaultHistoryRow {
    vault_id: string;
    created_time: number;
    type: "withdrawal" | "deposit";
    status: string;
    amount_change: number;
    token?: string; // need to hard code for now
    vaultName?: string; // need to hard code for now
  }

  export interface StrategyVaultHistory {
    rows: StrategyVaultHistoryRow[];
    meta: RecordsMeta;
  }

  export interface TransferHistoryRow {
    amount: number;
    created_time: number;
    from_account_id: string;
    id: string;
    status: "CREATED" | "PENDING" | "COMPLETED" | "FAILED";
    to_account_id: string;
    token: string;
    updated_time: number;
    chain_id: string;
    // timestamp
    block_time: number;
  }

  export interface TransferHistory {
    meta: RecordsMeta;
    rows: TransferHistoryRow[];
  }

  export interface TransferHistoryRow {
    amount: number;
    created_time: number;
    from_account_id: string;
    id: string;
    status: "CREATED" | "PENDING" | "COMPLETED" | "FAILED";
    to_account_id: string;
    token: string;
    updated_time: number;
  }

  export interface DailyRow {
    account_value: number;
    broker_id: string;
    date: string;
    perp_volume: number;
    pnl: number;
    snapshot_time?: number;
  }

  export interface PositionHistory {
    position_id: number; // Unique identifier for the position
    liquidation_id?: number; // Unique identifier for the position
    position_status: string; // Status of the position
    type: string; // Type of the position activity
    symbol: string; // Trading pair symbol
    avg_open_price: number; // Average open price of the position
    avg_close_price: number; // Average close price of the position
    max_position_qty: number; // Maximum quantity held in the position
    closed_position_qty: number; // Quantity closed in the position
    side: "LONG" | "SHORT"; // Side of the position
    trading_fee: number; // Fee charged for trading
    accumulated_funding_fee: number; // Accumulated funding fee for the position
    insurance_fund_fee: number; // Fee contributed to the insurance fund
    liquidator_fee: number; // Fee paid to the liquidator
    realized_pnl: number; // Realized profit and loss
    open_timestamp: number; // Timestamp when the position was opened
    close_timestamp: number; // Timestamp when the position was closed
    last_update_time: number; // Timestamp of the last update to the position
    leverage: number; // Leverage of the position
    margin_mode?: MarginMode | 1 | 0;
  }

  export interface LiquidationPositionByPerp {
    abs_liquidation_fee: number;
    cost_position_transfer: number;
    liquidator_fee: number;
    position_qty: number;
    symbol: string;
    transfer_price: number;
    mark_price: number;
  }

  export interface Liquidation {
    liquidation_id: number;
    timestamp: number;
    transfer_amount_to_insurance_fund: number;
    margin_ratio: number;
    account_mmr: number;
    collateral_value: number;
    position_notional: number;
    positions_by_perp: LiquidationPositionByPerp[];
  }

  export interface VaultBalance {
    chain_id: string;
    token: string;
    balance: number;
    pending_rebalance: number;
  }

  export interface RestrictedAreas {
    invalid_web_country: string;
    invalid_web_city: string;
  }

  export interface IpInfo {
    ip: string;
    city: string;
    region: string;
    checked: boolean;
  }

  export interface LeverageInfo {
    symbol: string;
    leverage: number;
    margin_mode?: MarginMode;
  }

  export namespace Referral {
    /** /v1/referral/multi_level/volume_prerequisite */
    export interface VolumePrerequisite {
      required_volume: number;
      current_volume: number;
    }

    /** /v1/referral/multi_level/max_rebate_rate */
    export interface MaxRebateRate {
      max_rebate_rate: number;
      bonus_max_rebate_rate?: number;
      base_rebate_rate?: number;
    }

    /** /v1/referral/multi_level/referee_kickback_info */
    export interface RefereeKickbackInfo {
      referee_kickback_rate: number;
    }

    /** /v1/referral/multi_level/rebate_info */
    export interface MultiLevelRebateInfo {
      referral_code: string;
      max_rebate_rate: number;
      default_referee_rebate_rate: number;
      default_referee_kickback_ratio?: number;
      max_referee_kickback_ratio?: number;
      can_configure_kickback?: boolean;
      bonus_max_rebate_rate?: number;
      base_rebate_rate?: number;
      default_bonus_referee_rebate_rate?: number;
      direct_invites?: number;
      indirect_invites?: number;
      direct_volume?: number;
      indirect_volume?: number;
      direct_rebate?: number;
      indirect_rebate?: number;
      direct_bonus_rebate_rate?: number;
      direct_bonus_rebate?: number;
    }

    /** /v1/referral/multi_level/admin */
    export interface MultiLevelReferralConfig {
      enable: boolean;
      required_volume: number;
      max_rebate_rate: number;
    }

    /** /v1/referral/multi_level/statistics */
    export interface MultiLevelStatistics {
      direct_invites: number;
      indirect_invites: number;
      direct_traded: number;
      indirect_traded: number;
      direct_volume: number;
      indirect_volume: number;
      direct_rebate: number;
      indirect_rebate: number;
      direct_bonus_rebate?: number;
    }
  }
}

export declare namespace WSMessage {
  export interface Ticker {
    symbol: string;
    open: number;
    close: number;
    high: number;
    low: number;
    volume: number;
    amount: number;
    count: number;
    change: number;
    open_interest?: number;
    index_price?: number;
  }

  export interface MarkPrice {
    symbol: string;
    price: number;
  }

  export interface Position {
    symbol: string;
    marginMode?: MarginMode;
    marginToken?: string;
    positionQty: number;
    costPosition: number;
    lastSumUnitaryFunding: number;
    accruedFundingFee?: number;
    sumUnitaryFundingVersion: number;
    pendingLongQty: number;
    pendingShortQty: number;
    settlePrice: number;
    averageOpenPrice: number;
    unsettledPnl: number;
    pnl24H: number;
    fee24H: number;
    markPrice: number;
    estLiqPrice: number;
    version: number;
    /**
     * Timestamp when the position was opened (13-digit ms)
     */
    timestamp?: number;
    /**
     * Timestamp when the position was last updated (13-digit ms)
     */
    updatedTime?: number;
    imr: number;
    imrwithOrders: number;
    mmrwithOrders: number;
    mmr: number;
    leverage?: number;
    margin?: number;
  }

  export interface VaultBalance {
    chain_id: string;
    token: string;
    balance: number;
    pending_rebalance: number;
  }

  export interface Order {
    symbol: string;
    clientOrderId: string;
    orderId: number;
    type: string;
    side: string;
    quantity: number;
    price: number;
    tradeId: number;
    executedPrice: number;
    executedQuantity: number;
    fee: number;
    feeAsset: string;
    totalExecutedQuantity: number;
    avgPrice: number;
    status: string;
    reason: string;
    totalFee: number;
    visible: number;
    /**
     * update time
     */
    timestamp: number;
    reduceOnly: boolean;
    maker: boolean;
    marginMode?: MarginMode;
  }

  export interface Holding {
    holding: number;
    frozen: number;
    interest: number;
    pendingShortQty: number;
    pendingExposure: number;
    pendingLongQty: number;
    pendingLongExposure: number;
    version: number;
    staked: number;
    unbonding: number;
    vault: number;
    fee24H: number;
    markPrice: number;
  }

  export interface AlgoOrder {
    symbol: string;
    rootAlgoOrderId: number;
    parentAlgoOrderId: number;
    algoOrderId: number;
    status: string;
    algoType: string;
    side: string;
    quantity: number;
    triggerStatus: string;
    price: number;
    type: string;
    triggerTradePrice: number;
    triggerTime: number;
    tradeId: number;
    executedPrice: number;
    executedQuantity: number;
    fee: number;
    feeAsset: string;
    totalExecutedQuantity: number;
    averageExecutedPrice: number;
    totalFee: number;
    timestamp: number;
    visibleQuantity: number;
    reduceOnly: boolean;
    triggered: boolean;
    maker: boolean;
    rootAlgoStatus: string;
    algoStatus: string;
    marginMode?: MarginMode;
  }

  export interface Announcement {
    announcement_id: string;
    message: string;
    i18n: Record<PropertyKey, string | null>;
    url?: string | null;
    type: AnnouncementType | null;
    updated_time: number;
  }
}
