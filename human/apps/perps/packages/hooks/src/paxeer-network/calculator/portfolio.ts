import { pathOr } from "ramda";
import { account } from "@paxeer-network/perp";
import { API, EMPTY_LIST, MarginMode } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { CalculatorCtx, CalculatorScope } from "../../types";
import { createGetter } from "../../utils/createGetter";
import { mergeHoldingBalance } from "../../utils/mergeHoldingBalance";
import { parseHolding } from "../../utils/parseHolding";
import { Portfolio, useAppStore } from "../appStore";
import { BaseCalculator } from "./baseCalculator";
import { IndexPriceCalculatorName } from "./indexPrice";
import { MarketCalculatorName } from "./markPrice";

export const PortfolioCalculatorName = "portfolio";

class PortfolioCalculator extends BaseCalculator<any> {
  name = PortfolioCalculatorName;

  calc(scope: CalculatorScope, data: any, ctx: CalculatorCtx) {
    let markPrices: Record<PropertyKey, number>;
    let indexPrices: Record<PropertyKey, number>;
    const portfolio = this.getPortfolio(ctx);

    if (scope === CalculatorScope.MARK_PRICE) {
      markPrices = data;
    } else {
      markPrices = ctx.get<Record<string, number>>(
        (cache) => cache[MarketCalculatorName],
      );
    }

    if (scope === CalculatorScope.INDEX_PRICE) {
      indexPrices = data;
    } else {
      indexPrices = ctx.get<Record<string, number>>(
        (cache) => cache[IndexPriceCalculatorName],
      );
    }

    // if (scope === CalculatorScope.POSITION) {
    //   positions = data;
    // } else {
    const positions = ctx.get<API.PositionsTPSLExt>(
      (output: Record<string, any>) => output.positionCalculator_all,
    );
    // }

    let holding = portfolio.holding;

    if (
      scope === CalculatorScope.PORTFOLIO &&
      data.holding &&
      Array.isArray(holding)
    ) {
      if (Array.isArray(data.holding)) {
        holding = data.holding;
      } else {
        holding = holding.map((item) => {
          if (data.holding[item.token]) {
            return mergeHoldingBalance(item, data.holding[item.token]);
          }

          return item;
        });
      }
    }

    const accountInfo = ctx.accountInfo!;
    const symbolsInfo = ctx.symbolsInfo!;
    const tokensInfo = ctx.tokensInfo;

    return this.format({
      holding,
      positions,
      markPrices,
      accountInfo,
      symbolsInfo,
      indexPrices: indexPrices,
      tokensInfo: tokensInfo ?? EMPTY_LIST,
    });
  }

  private getPortfolio(ctx: CalculatorCtx) {
    return (
      ctx.get<Portfolio>((output) => output[this.name]) ||
      useAppStore.getState().portfolio
    );
  }

  private format(inputs: {
    holding?: API.Holding[] | ReadonlyArray<API.Holding>;
    positions: API.PositionsTPSLExt;
    markPrices: Record<string, number> | null;
    indexPrices: Record<string, number> | null;
    accountInfo: API.AccountInfo;
    symbolsInfo: Record<string, API.SymbolExt>;
    tokensInfo: API.Chain[] | ReadonlyArray<API.Chain>;
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
      !accountInfo
    ) {
      return null;
    }

    // const totallCrossUnsettledPnL1 = positions.rows.reduce(
    //   (sum, pos) =>
    //     pos.margin_mode === MarginMode.ISOLATED
    //       ? sum
    //       : sum + (pos.unsettled_pnl ?? 0),
    //   0,
    // );

    const unsettledPnL = pathOr(0, ["total_unsettled_pnl"])(positions);
    const unsettledCrossPnL = pathOr(0, ["total_unsettled_cross_pnl"])(
      positions,
    );
    const unrealizedPnL = pathOr(0, ["total_unreal_pnl"])(positions);

    const [USDC_holding, nonUSDC] = parseHolding(
      holding,
      indexPrices,
      tokensInfo as API.Token[],
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

    // TODO: op code
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
      usdcHolding: USDC_holding,
      freeCollateralUSDCOnly,
    };
  }

  update(
    data: { [K in keyof Portfolio]: number | Decimal } | null,
    scope: CalculatorScope,
  ) {
    if (data) {
      useAppStore.getState().actions.batchUpdateForPortfolio({
        totalCollateral: data.totalCollateral as Decimal,
        totalValue: data.totalValue as Decimal,
        freeCollateral: data.freeCollateral as Decimal,
        freeCollateralUSDCOnly: data.freeCollateralUSDCOnly as Decimal,
        availableBalance: data.availableBalance as number,
        totalUnrealizedROI: data.totalUnrealizedROI as number,
        unsettledPnL: data.unsettledPnL as number,
        holding: Array.isArray(data.holding)
          ? (data.holding as API.Holding[])
          : [],
        usdcHolding:
          typeof data.usdcHolding === "number"
            ? data.usdcHolding
            : data.usdcHolding.toNumber(),
      });
    }
  }
}

export { PortfolioCalculator };
