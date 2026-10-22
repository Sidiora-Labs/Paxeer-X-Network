import { Badge } from "../../kit/collection";

import {
  GatewayRpcError,
  GatewayUnavailableError,
  surfaceCapability,
} from "../../api/gateway";
import {
  PERPS_COLLATERAL_DECIMALS,
  PERPS_COLLATERAL_TOKEN,
  PERPS_USD_DECIMALS,
  perpsAccount,
  perpsMarkets,
  type PerpsAccount,
  type PerpsMarket,
} from "../../api/precompiles";
import { PrecompileAbiError } from "../../api/sdk";
import { ExplorerPanel, ExplorerTable } from "../../kit/explorer";
import { StateEmpty } from "../../kit/surface";
import { ActivityFeed, activitySide } from "../_markets/activity-feed";
import { SurfaceCapabilityGate } from "../_markets/capability";
import { firstParam, formatUnits, isEvmAddress, type MarketSearchParams } from "../_markets/format";
import { MarketFrame, MarketUnavailable } from "../_markets/frame";
import { ExchangeActions, type PerpsCollateral } from "./exchange-client";

export const dynamic = "force-dynamic";

const DESCRIPTION = "Perpetual futures through the perps precompile: USDL margin, market positions and orders, settled on chain.";
const COLLATERAL: PerpsCollateral = { token: PERPS_COLLATERAL_TOKEN, decimals: PERPS_COLLATERAL_DECIMALS, symbol: "USDL" };
// modules/perps/types/state.go OrderType order.
const ORDER_TYPES = ["Limit", "Stop limit", "Take profit", "Stop loss", "Market", "Stop market"] as const;

function usd(value: bigint): string {
  return `$${formatUnits(value, PERPS_USD_DECIMALS)}`;
}

function usdl(value: bigint): string {
  return `${formatUnits(value, PERPS_COLLATERAL_DECIMALS)} USDL`;
}

function marketLabel(markets: readonly PerpsMarket[], marketId: bigint): string {
  return markets.find((market) => market.marketId === marketId)?.symbol ?? `#${marketId.toString()}`;
}

function sideLabel(isLong: boolean): string {
  return isLong ? "Long" : "Short";
}

export default async function ExchangePage({ searchParams }: Readonly<{ searchParams: Promise<MarketSearchParams> }>) {
  const parameters = await searchParams;
  const candidate = firstParam(parameters.account)?.toLowerCase();
  const account = isEvmAddress(candidate) ? candidate : undefined;
  const capability = await surfaceCapability("exchange");
  const feed = (
    <ActivityFeed
      route="/exchange"
      account={account}
      side={activitySide(firstParam(parameters.side))}
      cursor={firstParam(parameters.cursor)}
      kind={firstParam(parameters.kind)}
    />
  );
  if (!capability.live) {
    return (
      <MarketFrame title="Exchange" description={DESCRIPTION} account={account}>
        <SurfaceCapabilityGate surface="exchange" live={false} detail={capability.detail} />
        {feed}
      </MarketFrame>
    );
  }
  let markets: readonly PerpsMarket[] = [];
  let state: PerpsAccount | undefined;
  let problem: string | undefined;
  try {
    [markets, state] = await Promise.all([perpsMarkets(), account === undefined ? undefined : perpsAccount(account)]);
  } catch (error) {
    if (
      !(error instanceof GatewayUnavailableError || error instanceof GatewayRpcError || error instanceof PrecompileAbiError)
    ) {
      throw error;
    }
    problem = error.message;
  }
  return (
    <MarketFrame title="Exchange" description={DESCRIPTION} account={account}>
      <SurfaceCapabilityGate surface="exchange" live detail={null} />
      {problem === undefined ? null : <MarketUnavailable detail={problem} />}
      <ExplorerPanel title="Markets">
        {markets.length === 0 ? (
          <StateEmpty title="No markets" description="Markets the perps precompile lists appear here." />
        ) : (
          <ExplorerTable
            caption="Markets"
            columns={["Market", "Name", "Max leverage", "Maintenance margin", "Max open interest", "Status"]}
            rows={markets.map((market) => ({
              id: market.marketId.toString(),
              cells: [
                market.symbol,
                market.name,
                `${market.maxLeverage.toString()}x`,
                `${market.maintenanceMarginBps.toString()} bps`,
                usd(market.maxOpenInterest),
                <Badge key="status" variant={market.enabled ? "success" : "warning"}>
                  {market.enabled ? "Enabled" : "Disabled"}
                </Badge>,
              ],
            }))}
          />
        )}
      </ExplorerPanel>
      {state === undefined ? null : (
        <ExplorerPanel title="Margin account">
          <ExplorerTable
            caption="Margin account"
            columns={["Idle", "Locked in positions"]}
            rows={[{ id: "account", cells: [usdl(state.idle), usdl(state.locked)] }]}
          />
        </ExplorerPanel>
      )}
      <ExplorerPanel title="Positions">
        {state === undefined || state.positions.length === 0 ? (
          <StateEmpty title="No open positions" description="Positions this wallet holds on the perps venue appear here." />
        ) : (
          <ExplorerTable
            caption="Positions"
            columns={["Position", "Market", "Side", "Size", "Entry price", "Collateral"]}
            rows={state.positions.map((position) => ({
              id: position.positionId.toString(),
              cells: [
                `#${position.positionId.toString()}`,
                marketLabel(markets, position.marketId),
                sideLabel(position.isLong),
                usd(position.sizeUsd),
                usd(position.entryPrice),
                usdl(position.collateralAmount),
              ],
            }))}
          />
        )}
      </ExplorerPanel>
      <ExplorerPanel title="Open orders">
        {state === undefined || state.orders.length === 0 ? (
          <StateEmpty title="No open orders" description="Active orders this wallet holds on the perps venue appear here." />
        ) : (
          <ExplorerTable
            caption="Open orders"
            columns={["Order", "Market", "Side", "Type", "Trigger price", "Limit price", "Size", "Collateral"]}
            rows={state.orders.map((order) => ({
              id: order.orderId.toString(),
              cells: [
                `#${order.orderId.toString()}`,
                marketLabel(markets, order.marketId),
                sideLabel(order.isLong),
                ORDER_TYPES[Number(order.orderType)] ?? order.orderType.toString(),
                usd(order.triggerPrice),
                usd(order.limitPrice),
                usd(order.sizeUsd),
                usdl(order.collateralAmount),
              ],
            }))}
          />
        )}
      </ExplorerPanel>
      <ExchangeActions
        account={account}
        collateral={COLLATERAL}
        markets={markets
          .filter((market) => market.enabled)
          .map((market) => ({ id: market.marketId.toString(), label: `${market.symbol} (${market.name})` }))}
        positions={(state?.positions ?? []).map((position) => ({
          id: position.positionId.toString(),
          label: `#${position.positionId.toString()} ${sideLabel(position.isLong)} ${marketLabel(markets, position.marketId)} ${usd(position.sizeUsd)}`,
        }))}
        orders={(state?.orders ?? []).map((order) => ({
          id: order.orderId.toString(),
          label: `#${order.orderId.toString()} ${sideLabel(order.isLong)} ${marketLabel(markets, order.marketId)} ${usd(order.sizeUsd)}`,
        }))}
      />
      {feed}
    </MarketFrame>
  );
}
