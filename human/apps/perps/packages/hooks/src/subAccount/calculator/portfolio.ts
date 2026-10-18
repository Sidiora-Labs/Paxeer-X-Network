import { pathOr } from "ramda";
import { account } from "@paxeer-network/perp";
import { API, MarginMode } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { SymbolsInfo } from "../../paxeer-network/useSymbolsInfo";
import { createGetter } from "../../utils/createGetter";
import { parseHolding } from "../../utils/parseHolding";

export type Portfolio = {
  holding?: API.Holding[];
  totalCollateral: Decimal;
  freeCollateral: Decimal;
  freeCollateralUSDCOnly: Decimal;
  totalValue: Decimal | null;
  availableBalance: number;
  unsettledPnL: number;
  totalUnrealizedROI: number;
};

export function formatPortfolio(inputs: {
  holding?: API.Holding[];
  positions?: API.PositionsTPSLExt;
  markPrices: Record<string, number> | null;
  indexPrices?: Record<string, number> | null;
  accountInfo?: API.AccountInfo;
  symbolsInfo?: SymbolsInfo;
  tokensInfo?: API.Token[];
}) {
  const {
    holding,
    positions,
    markPrices,
    indexPrices,
    accountInfo,
    symbolsInfo,
    tokensInfo,
  } = inputs;

  if (
    !holding ||
    !positions ||
    !Array.isArray(positions.rows) ||
    !markPrices ||
    !indexPrices ||
    !accountInfo ||
    symbolsInfo?.isNil
  ) {
    return null;
  }

  const unsettledPnL = pathOr(0, ["total_unsettled_pnl"])(positions);
  const unsettledCrossPnL = pathOr(0, ["total_unsettled_cross_pnl"])(positions);
  const unrealizedPnL = pathOr(0, ["total_unreal_pnl"])(positions);

  const [USDC_holding, nonUSDC] = parseHolding(
    holding,
    indexPrices,
    tokensInfo!,
  );

  const usdc = holding.find((item) => item.token === "USDC");

  const totalCollateral = account.totalCollateral({
    USDCHolding: USDC_holding,
    nonUSDCHolding: nonUSDC,
    unsettlementPnL: unsettledCrossPnL,
    usdcBalancePendingShortQty: usdc?.pending_short ?? 0,
    usdcBalanceIsolatedOrderFrozen: usdc?.isolated_order_frozen ?? 0,
  });

  const sumIsolatedMargin = positions.rows.reduce<Decimal>((acc, curr) => {
    if (curr.margin_mode !== MarginMode.ISOLATED) {
      return acc;
    }
    return acc.add(curr.margin ?? 0);
  }, zero);

  const totalValue = account.totalValue({
    totalUnsettlementPnL: unsettledPnL,
    USDCHolding: USDC_holding,
    nonUSDCHolding: nonUSDC,
    totalIsolatedPositionMargin: sumIsolatedMargin.toNumber(),
  });

  const totalUnrealizedROI = account.totalUnrealizedROI({
    totalUnrealizedPnL: unrealizedPnL,
    totalValue: totalValue.toNumber(),
  });
  const maxLeverageBySymbol = positions.rows.reduce<Record<string, number>>(
    (acc, position) => {
      if (
        position.margin_mode !== MarginMode.ISOLATED &&
        position.leverage &&
        !acc[position.symbol]
      ) {
        acc[position.symbol] = position.leverage;
      }
      return acc;
    },
    {},
  );

  const totalInitialMarginWithOrders = account.totalInitialMarginWithQty({
    positions: positions.rows,
    markPrices,
    IMR_Factors: accountInfo.imr_factor,
    maxLeverageBySymbol,
    symbolInfo: createGetter({ ...symbolsInfo }),
  });

  const freeCollateral = account.freeCollateral({
    totalCollateral,
    totalInitialMarginWithOrders,
  });

  const freeCollateralUSDCOnly = account.freeCollateralUSDCOnly({
    freeCollateral,
    nonUSDCHolding: nonUSDC,
  });

  const availableBalance = account.availableBalance({
    USDCHolding: usdc?.holding ?? 0,
    unsettlementPnL: positions.total_unsettled_pnl ?? 0,
  });

  return {
    totalCollateral,
    totalValue,
    totalUnrealizedROI,
    freeCollateral,
    availableBalance,
    unsettledPnL,
    holding,
    freeCollateralUSDCOnly,
  };
}
