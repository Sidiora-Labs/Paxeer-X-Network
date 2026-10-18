import { propOr } from "ramda";
import { account, positions } from "@paxeer-network/perp";
import { API, MarginMode } from "@paxeer-network/types";
import { zero } from "@paxeer-network/utils";
import { SymbolsInfo } from "../../paxeer-network/useSymbolsInfo";

export function formatPositions(
  data: API.PositionInfo | API.PositionsTPSLExt,
  accountInfo?: API.AccountInfo,
  symbolsInfo?: SymbolsInfo,
  fundingRates?: Record<string, API.FundingRate>,
): API.PositionsTPSLExt {
  if (!accountInfo || !fundingRates || !symbolsInfo) {
    return data as API.PositionsTPSLExt;
  }

  let unrealPnL_total = zero,
    unrealPnL_total_index = zero,
    notional_total = zero,
    unsettlementPnL_total = zero,
    totalUnsettledIsolatedPnl = zero,
    totalUnsettledCrossPnl = zero;

  const rows = data.rows.map((item) => {
    const info = symbolsInfo[item.symbol];

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
      baseIMR: info?.("base_imr"),
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
        fundingRates?.[item.symbol],
      ),
      lastSumUnitaryFunding: item.last_sum_unitary_funding,
    });

    const MMR = positions.MMR({
      baseMMR: info?.("base_mmr"),
      baseIMR: info?.("base_imr"),
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

    return {
      ...item,
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

  return {
    ...data,

    unrealPnL: totalUnrealPnl,
    total_unreal_pnl: totalUnrealPnl,
    total_unreal_pnl_index: totalUnrealPnl_index,
    notional: notional_total.toNumber(),

    unsettledPnL: unsettlementPnL,
    total_unsettled_pnl: unsettlementPnL,
    total_unsettled_cross_pnl: totalUnsettledCrossPnl.toNumber(),
    total_unsettled_isolated_pnl: totalUnsettledIsolatedPnl.toNumber(),
    rows,
  };
}

export function calcByPrice(
  positions?: API.PositionInfo,
  markPrice?: Record<string, number>,
  indexPrice?: Record<string, number>,
) {
  if (!positions || !Array.isArray(positions.rows) || !positions.rows.length) {
    return positions;
  }

  return {
    ...positions,
    rows: positions.rows.map((item) => ({
      ...item,
      mark_price: markPrice?.[item.symbol] || item.mark_price,
      index_price:
        indexPrice?.[item.symbol] || item.index_price || item.mark_price,
    })),
  };
}
