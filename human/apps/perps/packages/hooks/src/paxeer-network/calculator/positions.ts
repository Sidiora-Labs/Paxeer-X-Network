import { propOr } from "ramda";
import { account, positions } from "@paxeer-network/perp";
import { API, MarginMode } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { useApiStatusStore } from "../../next/apiStatus/apiStatus.store";
import { CalculatorCtx, CalculatorScope } from "../../types";
import { useAppStore } from "../appStore";
import { usePositionStore } from "../usePositionStream/usePosition.store";
import { BaseCalculator } from "./baseCalculator";
import { CalculatorContext } from "./calculatorContext";

const NAME_PREFIX = "positionCalculator";
const AllPositions = "all";

class PositionCalculator extends BaseCalculator<API.PositionInfo> {
  // name = "positionCalculator";
  name: string;
  // private id!: string;

  // private state;
  private symbol: string;

  // private id: string;

  constructor(symbol: string = AllPositions) {
    super();

    this.name = `${NAME_PREFIX}_${symbol}`;

    this.symbol = symbol;
  }

  calc(
    scope: CalculatorScope,
    data: any,
    ctx: CalculatorCtx,
  ): API.PositionsTPSLExt | null {
    if (scope === CalculatorScope.MARK_PRICE) {
      return this.calcByMarkPrice(data as Record<string, number>, ctx);
    }

    if (scope === CalculatorScope.INDEX_PRICE) {
      return this.calcByIndexPrice(data, ctx);
    }

    if (scope === CalculatorScope.POSITION) {
      return this.calcByPosition(
        this.preprocess(data as API.PositionInfo),
        ctx,
      );
    }

    return data;
  }

  update(data: API.PositionsTPSLExt | null, scope: CalculatorScope) {
    // if (this.symbol !== AllPositions) {
    //   console.log("PositionCalculator update", scope, this.name, data);
    // }
    if (!data || !Array.isArray(data.rows)) return;

    usePositionStore.getState().actions.setPositions(this.symbol, data);

    /// update position loading status
    if (
      Array.isArray(data.rows) &&
      useApiStatusStore.getState().apis.positions.loading
    ) {
      useApiStatusStore.getState().actions.updateApiLoading("positions", false);
    }
  }

  private calcByMarkPrice(
    markPrice: Record<string, number>,
    ctx: CalculatorCtx,
  ) {
    let positions = this.getPosition(markPrice, ctx);
    const fundingRates = useAppStore.getState().fundingRates;

    // positions = this.checkIsClosed(positions, ctx);

    // console.log("-------PositionCalculator calcByMarkPrice", positions);

    if (!positions || !Array.isArray(positions.rows) || !positions.rows.length)
      return positions;

    positions = {
      ...positions,
      rows: positions.rows.map((item: API.PositionTPSLExt) => {
        // console.log(fundingRates?.[item.symbol]);

        return {
          ...item,

          mark_price: markPrice[item.symbol] || item.mark_price,
        };
      }),
    };

    return this.format(positions, ctx);
  }

  private calcByIndexPrice(
    indexPrice: Record<string, number>,
    ctx: CalculatorCtx,
  ) {
    let positions = this.getPosition(indexPrice, ctx);
    // positions = this.checkIsClosed(positions, ctx);

    if (!positions) {
      return positions;
    }

    if (!Array.isArray(positions.rows) || !positions.rows.length)
      return positions;

    positions = {
      ...positions,
      rows: positions.rows.map((item: API.PositionTPSLExt) => ({
        ...item,
        index_price:
          indexPrice[item.symbol] || item.index_price || item.mark_price,
      })),
    };

    return this.format(positions, ctx);
  }

  private calcByPosition(positions: API.PositionInfo, ctx: CalculatorCtx) {
    if (positions.rows.length === 0) return positions as API.PositionsTPSLExt;

    // need to clear the position if the position has been closed
    return this.format(positions, ctx);
  }

  private format(
    data: API.PositionInfo | API.PositionsTPSLExt,
    ctx: CalculatorCtx,
  ): API.PositionsTPSLExt {
    const { accountInfo, symbolsInfo, fundingRates, portfolio } = ctx;

    if (!accountInfo || !fundingRates || !symbolsInfo) {
      return data as API.PositionsTPSLExt;
    }

    let unrealPnL_total = zero,
      unrealPnL_total_index = zero,
      notional_total = zero,
      unsettlementPnL_total = zero,
      totalUnsettledIsolatedPnl = zero,
      totalUnsettledCrossPnl = zero;

    let rows = data.rows.map((item) => {
      const info = symbolsInfo[item.symbol];
      const sum_unitary_funding =
        fundingRates?.[item.symbol]?.["sum_unitary_funding"] ?? 0;

      const notional = positions.notional(item.position_qty, item.mark_price);
      const unrealPnl = positions.unrealizedPnL({
        qty: item.position_qty,
        openPrice: item?.average_open_price,
        // markPrice: unRealizedPrice,
        markPrice: item.mark_price,
      });
      let unrealPnl_index = 0,
        unrealPnlROI_index = 0;

      const maxLeverage = item.leverage || 1;

      const imr = account.IMR({
        maxLeverage,
        baseIMR: info?.["base_imr"],
        IMR_Factor: accountInfo.imr_factor[item.symbol] as number,
        positionNotional: notional,
        ordersNotional: 0,
        IMR_factor_power: 4 / 5,
      });

      const unrealPnlROI = positions.unrealizedPnLROI({
        positionQty: item.position_qty,
        openPrice: item.average_open_price,
        IMR: imr,
        unrealizedPnL: unrealPnl,
      });

      if (item.index_price) {
        unrealPnl_index = positions.unrealizedPnL({
          qty: item.position_qty,
          openPrice: item?.average_open_price,
          // markPrice: unRealizedPrice,
          markPrice: item.index_price,
        });

        unrealPnlROI_index = positions.unrealizedPnLROI({
          positionQty: item.position_qty,
          openPrice: item.average_open_price,
          IMR: imr,
          unrealizedPnL: unrealPnl_index,
        });
      }

      const unsettlementPnL = positions.unsettlementPnL({
        positionQty: item.position_qty,
        markPrice: item.mark_price,
        costPosition: item.cost_position,
        sumUnitaryFunding: propOr(
          0,
          "sum_unitary_funding",
          fundingRates[item.symbol],
        ),
        lastSumUnitaryFunding: item.last_sum_unitary_funding,
      });

      const MMR = positions.MMR({
        baseMMR: info?.["base_mmr"],
        baseIMR: info?.["base_imr"],
        IMRFactor: accountInfo.imr_factor[item.symbol] as number,
        positionNotional: notional,
        IMR_factor_power: 4 / 5,
      });

      unrealPnL_total = unrealPnL_total.add(unrealPnl);
      unrealPnL_total_index = unrealPnL_total_index.add(unrealPnl_index);
      notional_total = notional_total.add(notional);
      unsettlementPnL_total = unsettlementPnL_total.add(unsettlementPnL);

      if (item.margin_mode === MarginMode.CROSS) {
        totalUnsettledCrossPnl = totalUnsettledCrossPnl.add(unsettlementPnL);
      }

      if (item.margin_mode === MarginMode.ISOLATED) {
        totalUnsettledIsolatedPnl =
          totalUnsettledIsolatedPnl.add(unsettlementPnL);
      }

      const fundingFee = new Decimal(sum_unitary_funding)
        .sub(item.last_sum_unitary_funding)
        .mul(item.position_qty)
        .negated()
        .toNumber();

      return {
        ...item,
        fundingFee,
        mm: positions.maintenanceMargin({
          positionQty: item.position_qty,
          markPrice: item.mark_price,
          MMR,
        }),
        mmr: MMR,
        notional,
        unsettlement_pnl: unsettlementPnL,
        unrealized_pnl: unrealPnl,
        unrealized_pnl_index: unrealPnl_index,
        unrealized_pnl_ROI: unrealPnlROI,
        unrealized_pnl_ROI_index: unrealPnlROI_index,
        unsettled_pnl: unsettlementPnL,
      };
    });

    const totalUnrealPnl = unrealPnL_total.toNumber();
    const totalUnrealPnl_index = unrealPnL_total_index.toNumber();
    const unsettlementPnL = unsettlementPnL_total.toNumber();
    let totalUnrealizedROI = 0,
      totalUnrealizedROI_index = 0;

    if (portfolio) {
      const { totalValue, totalCollateral: crossMarginCollateral } = portfolio;

      rows = rows.map((item) => {
        const info = symbolsInfo[item.symbol];
        // NOTE: Calculate totalCollateral based on marginMode.
        // If margin mode is ISOLATED, use isolated margin plus unsettlementPnL;
        // otherwise (CROSS margin), use account total collateral value.
        const totalCollateral =
          item.margin_mode === MarginMode.ISOLATED
            ? new Decimal(item.margin ?? 0)
                .add(item.unsettlement_pnl ?? 0)
                .toNumber()
            : crossMarginCollateral.toNumber();

        let est_liq_price;
        if (item.margin_mode === MarginMode.ISOLATED) {
          est_liq_price = positions.liquidationPriceIsolated({
            isolatedPositionMargin: item.margin ?? 0,
            costPosition: item.cost_position ?? 0,
            positionQty: item.position_qty ?? 0,
            sumUnitaryFunding:
              fundingRates?.[item.symbol]?.["sum_unitary_funding"] ?? 0,
            lastSumUnitaryFunding: item.last_sum_unitary_funding ?? 0,
            baseMMR: info?.["base_mmr"] ?? 0,
            baseIMR: info?.["base_imr"] ?? 0,
            IMRFactor: accountInfo.imr_factor[item.symbol] as number,
            referencePrice: item.mark_price,
            leverage: item.leverage ?? 0,
          });
        } else {
          est_liq_price = positions.liqPrice({
            symbol: item.symbol,
            markPrice: item.mark_price,
            totalCollateral: totalCollateral,
            positionQty: item.position_qty,
            positions: rows,
            MMR: item.mmr,
            baseMMR: info?.["base_mmr"],
            baseIMR: info?.["base_imr"],
            IMRFactor: accountInfo.imr_factor[item.symbol] as number,
            costPosition: item.cost_position,
          });
        }
        return {
          ...item,
          est_liq_price,
        };
      });

      if (totalValue !== null && !totalValue.eq(zero)) {
        totalUnrealizedROI = account.totalUnrealizedROI({
          totalUnrealizedPnL: totalUnrealPnl,
          totalValue: totalValue.toNumber(),
        });
        totalUnrealizedROI_index = account.totalUnrealizedROI({
          totalUnrealizedPnL: totalUnrealPnl_index,
          totalValue: totalValue.toNumber(),
        });
      }
    }

    return {
      ...data,

      unrealPnL: totalUnrealPnl,
      total_unreal_pnl: totalUnrealPnl,
      total_unreal_pnl_index: totalUnrealPnl_index,
      notional: notional_total.toNumber(),

      unsettledPnL: unsettlementPnL,
      total_unsettled_pnl: unsettlementPnL,
      unrealPnlROI: totalUnrealizedROI,
      unrealPnlROI_index: totalUnrealizedROI_index,
      total_unsettled_cross_pnl: totalUnsettledCrossPnl.toNumber(),
      total_unsettled_isolated_pnl: totalUnsettledIsolatedPnl.toNumber(),
      rows,
    };
  }

  private preprocess(data: API.PositionInfo): API.PositionInfo {
    let rows = data.rows;
    if (this.symbol !== AllPositions && Array.isArray(rows)) {
      rows = rows.filter((item: API.Position) => item.symbol === this.symbol);
    }
    return {
      ...data,
      rows,
    };
  }

  private getPosition(_: Record<string, number>, ctx: CalculatorCtx) {
    const positions =
      ctx.get((output: Record<string, any>) => output[this.name]) ||
      usePositionStore.getState().positions[this.symbol];

    if (this.symbol === AllPositions) {
      return positions;
    }

    if (positions && Array.isArray(positions.rows)) {
      return positions;
    }

    return this.preprocess(this.getAllPositions(ctx) as API.PositionInfo);
  }

  destroy() {
    usePositionStore.getState().actions.closePosition(this.symbol);
    CalculatorContext.instance?.deleteByName(this.name);
  }

  private getAllPositions(ctx: CalculatorCtx) {
    return (
      ctx.get((output: Record<string, any>) => output[AllPositions]) ||
      usePositionStore.getState().positions[AllPositions]
    );
  }

  static logPosition = (symbol = "all") => {
    return usePositionStore.getState().positions[symbol];
  };
}

// const usePositionCalculator = () => new PositionCalculator();

export { PositionCalculator };
