# api

## Overview

Defines the `API` and `WSMessage` namespaces used across the paxeer-network ecosystem. `API` describes REST response shapes for public and private endpoints (tokens, markets, symbols, orders, positions, funding, account, asset history, referrals, etc.). `WSMessage` describes WebSocket message payloads (ticker, mark price, position, order, algo order, holding, announcement). Also exports `AnnouncementType` enum.

## Exports

### AnnouncementType (enum)

| Value | Description |
|-------|-------------|
| Listing | LISTING |
| Maintenance | MAINTENANCE |
| Delisting | DELISTING |
| Campaign | CAMPAIGN |
| Vote | VOTE |

## API namespace (REST)

Main interfaces (grouped by domain):

### Token / Chain

- **ConvertThreshold** – ltv_threshold, negative_usdc_threshold
- **Token** – token, token_hash, decimals, minimum_withdraw_amount, base_weight, discount_factor, haircut, user_max_qty, is_collateral, on_chain_swap, chain_details
- **ChainDetail** – chain_id, contract_address, decimals (and variants with withdrawal_fee, cross_chain_withdrawal_fee, display_name, chain_name)
- **TokenItem** – token, token_hash, decimals, minimum_withdraw_amount, chain_details

### Market / Symbol

- **MarketInfo** – symbol, index_price, mark_price, sum_unitary_funding, est_funding_rate, last_funding_rate, next_funding_time, open_interest, 24h_*, etc.
- **MarketInfoExt** – MarketInfo + change, 24h_volume
- **Symbol** – symbol, quote_min/max/tick, base_min/max/tick, min_notional, price_range, funding_period, base_mmr, base_imr, imr_factor, etc.
- **SymbolExt** – Symbol + base, quote, base_dp, quote_dp, type, name, displayName
- **RwaSymbol** – symbol, status (open/close), next_open, next_close, base, quote, type, name

### Announcement

- **AnnouncementRow** – announcement_id, message, i18n?, url?, type?, updated_time?, coverImage?
- **Announcement** – last_updated_time?, rows?

### Order (REST)

- **Order** – symbol, status, side, order_id, algo_order_id?, user_id, price, type, quantity, visible, executed, total_fee, fee_asset, client_order_id?, average_executed_price, total_executed_quantity, visible_quantity, created_time, updated_time, reduce_only, trigger_price?, order_tag?
- **OrderExt** – Order + mark_price
- **AlgoOrder** – algo_order_id, root_algo_order_id, parent_algo_order_id, parent_algo_type, symbol, algo_type, child_orders, side, quantity, is_triggered, is_activated, trigger_price, trigger_price_type, type, root_algo_status, algo_status, price?, total_executed_quantity, visible_quantity, total_fee, fee_asset, reduce_only, created_time, updated_time, order_tag?, client_order_id?, activated_price?, callback_value?, callback_rate?, extreme_price?
- **AlgoOrderExt** – AlgoOrder + mark_price, position?, tp_trigger_price?, sl_trigger_price?
- **OrderResponse** – rows: (Order | AlgoOrder)[], meta: { total, current_page, records_per_page }

### Funding

- **FundingRate** – symbol, est_funding_rate, est_funding_rate_timestamp, last_funding_rate, last_funding_rate_timestamp, next_funding_time, sum_unitary_funding
- **FundingPeriodData** – rate, positive, negative
- **FundingDetails** – symbol and symbol config fields (quote_min/max/tick, base_*, min_notional, funding_period, liquidation_tier, etc.)
- **FundingHistory** – symbol, data_start_time, funding: { last, 1d, 3d, 7d, 14d, 30d, 90d }
- **FundingFeeHistory** – meta, rows: FundingFeeRow[]
- **FundingFeeRow** – symbol, funding_rate, mark_price, funding_fee, payment_type, status, created_time, updated_time

### Position (REST)

- **Position** – symbol, position_qty, cost_position, last_sum_unitary_funding, pending_long_qty, pending_short_qty, settle_price, average_open_price, unrealized_pnl, unsettled_pnl, mark_price, est_liq_price, timestamp, mmr, imr, leverage, etc.
- **PositionExt** – Position + notional, mm
- **PositionTPSLExt** – PositionExt + full_tp_sl?, partial_tp_sl?, algo_order?
- **PositionAggregated** – margin_ratio, initial_margin_ratio, maintenance_margin_ratio, total_collateral_value, free_collateral, total_pnl_24_h, unrealPnL/total_unreal_pnl, unsettledPnL/total_unsettled_pnl, notional, unrealPnlROI, etc.
- **PositionInfo** – PositionAggregated + rows: Position[]
- **PositionsTPSLExt** – PositionAggregated + rows: PositionTPSLExt[]

### Trade / Holding / Account

- **Trade** – symbol (Symbol), side (OrderSide), ts, executed_price, executed_quantity, executed_timestamp
- **Holding** – token, holding, frozen, pending_short, updated_time
- **AccountInfo** – account_id, email, account_mode, tier, futures_tier, maintenance_cancel_orders, taker_fee_rate, maker_fee_rate, rwa_*_fee_rate?, max_leverage, futures_taker_fee_rate, futures_maker_fee_rate, imr_factor, max_notional

### Chain / Network (API)

- **Chain** (API) – dexs, network_infos (NetworkInfos), token_infos (TokenInfo[]), nativeToken?, address?, symbol?, on_chain_swap?
- **NetworkInfos** – name, shortName, public_rpc_url, chain_id, currency_symbol, bridge_enable, mainnet, est_txn_mins, explorer_base_url, bridgeless?, withdrawal_fee?, minimum_withdraw_amount?, vault_address, currency_decimal?, cross_chain_router, depositor
- **TokenInfo** – address?, base_weight, decimals?, token_decimal?, discount_factor?, display_name?, haircut, is_collateral, symbol?, user_max_qty, precision?, minimum_withdraw_amount, swap_enable?
- **Chain** (token variant) – token, token_hash, decimals, minimum_withdraw_amount, base_weight, discount_factor?, haircut, user_max_qty, is_collateral, chain_details

### Asset / Transfer / History

- **AssetHistory** – meta: RecordsMeta, rows: AssetHistoryRow[]
- **RecordsMeta** – total, records_per_page, current_page
- **AssetHistoryRow** – id, tx_id, side, token, amount, fee, trans_status, created_time, updated_time, chain_id
- **TransferHistoryRow** – amount, created_time, from_account_id, id, status (CREATED/PENDING/COMPLETED/FAILED), to_account_id, token, updated_time, chain_id?, block_time?
- **TransferHistory** – meta, rows: TransferHistoryRow[]
- **StrategyVaultHistoryRow** – vault_id, created_time, type (withdrawal/deposit), status, amount_change, token?, vaultName?
- **StrategyVaultHistory** – rows, meta

### PnL / Liquidation / Vault / Misc

- **DailyRow** – account_value, broker_id, date, perp_volume, pnl, snapshot_time?
- **PositionHistory** – position_id, liquidation_id?, position_status, type, symbol, avg_open_price, avg_close_price, max_position_qty, closed_position_qty, side (LONG/SHORT), trading_fee, accumulated_funding_fee, insurance_fund_fee, liquidator_fee, realized_pnl, open_timestamp, close_timestamp, last_update_time, leverage
- **LiquidationPositionByPerp** – abs_liquidation_fee, cost_position_transfer, liquidator_fee, position_qty, symbol, transfer_price, mark_price
- **Liquidation** – liquidation_id, timestamp, transfer_amount_to_insurance_fund, margin_ratio, account_mmr, collateral_value, position_notional, positions_by_perp
- **VaultBalance** – chain_id, token, balance, pending_rebalance
- **RestrictedAreas** – invalid_web_country, invalid_web_city
- **IpInfo** – ip, city, region, checked
- **LeverageInfo** – symbol, leverage

### Referral (API.Referral)

- **VolumePrerequisite** – required_volume, current_volume
- **MaxRebateRate** – max_rebate_rate
- **MultiLevelRebateInfo** – referral_code, max_rebate_rate, default_referee_rebate_rate, direct_invites?, indirect_invites?, direct_volume?, indirect_volume?, direct_rebate?, indirect_rebate?, direct_bonus_rebate_rate?, direct_bonus_rebate?
- **MultiLevelReferralConfig** – enable, required_volume, max_rebate_rate
- **MultiLevelStatistics** – direct_invites, indirect_invites, direct_traded, indirect_traded, direct_volume, indirect_volume, direct_rebate, indirect_rebate, direct_bonus_rebate?

## WSMessage namespace (WebSocket)

- **Ticker** – symbol, open, close, high, low, volume, amount, count, change, open_interest?, index_price?
- **MarkPrice** – symbol, price
- **Position** – symbol, positionQty, costPosition, lastSumUnitaryFunding, sumUnitaryFundingVersion, pendingLongQty, pendingShortQty, settlePrice, averageOpenPrice, unsettledPnl, pnl24H, fee24H, markPrice, estLiqPrice, version, imr, imrwithOrders, mmrwithOrders, mmr
- **VaultBalance** – chain_id, token, balance, pending_rebalance
- **Order** – symbol, clientOrderId, orderId, type, side, quantity, price, tradeId, executedPrice, executedQuantity, fee, feeAsset, totalExecutedQuantity, avgPrice, status, reason, totalFee, visible, timestamp, reduceOnly, maker
- **Holding** – holding, frozen, interest, pendingShortQty, pendingExposure, pendingLongQty, pendingLongExposure, version, staked, unbonding, vault, fee24H, markPrice
- **AlgoOrder** – symbol, rootAlgoOrderId, parentAlgoOrderId, algoOrderId, status, algoType, side, quantity, triggerStatus, price, type, triggerTradePrice, triggerTime, tradeId, executedPrice, executedQuantity, fee, feeAsset, totalExecutedQuantity, averageExecutedPrice, totalFee, timestamp, visibleQuantity, reduceOnly, triggered, maker, rootAlgoStatus, algoStatus
- **Announcement** – announcement_id, message, i18n, url?, type, updated_time

## Usage example

```typescript
import { API, WSMessage, AnnouncementType } from "@paxeer-network/types";

const market: API.MarketInfo = await fetchMarket(symbol);
const pos: API.Position = positionResponse.rows[0];
const wsOrder: WSMessage.Order = msg.data;
```
