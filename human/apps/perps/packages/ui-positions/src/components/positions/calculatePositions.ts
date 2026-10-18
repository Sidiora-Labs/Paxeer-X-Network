import {
  findPositionTPSLFromOrders,
  findTPSLFromOrder,
  SymbolsInfo,
} from "@paxeer-network/hooks";
import {
  positions as positionsPerp,
  account as accountPerp,
} from "@paxeer-network/perp";
import { API, MarginMode } from "@paxeer-network/types";

export const calculatePositions = (
  positions: API.PositionExt[],
  symbolsInfo: SymbolsInfo,
  accountInfo: API.AccountInfo[],
  tpslOrders: (API.AlgoOrder & { account_id: string })[],
): API.PositionTPSLExt[] => {
  return positions.map((item) => {
    const info = symbolsInfo[item.symbol];
    const notional = positionsPerp.notional(item.position_qty, item.mark_price);
    const account = accountInfo.find(
      (acc) => acc.account_id === item.account_id,
    );
    const baseMMR = info?.("base_mmr");
    const baseIMR = info?.("base_imr");

    if (!baseMMR || !baseIMR) {
      return item as API.PositionTPSLExt;
    }

    const MMR = positionsPerp.MMR({
      baseMMR,
      baseIMR,
      IMRFactor: account?.imr_factor[item.symbol] ?? 0,
      positionNotional: notional,
      IMR_factor_power: 4 / 5,
    });

    const mm = positionsPerp.maintenanceMargin({
      positionQty: item.position_qty,
      markPrice: item.mark_price,
      MMR,
    });

    const unrealPnl = positionsPerp.unrealizedPnL({
      qty: item.position_qty,
      openPrice: item?.average_open_price,
      markPrice: item.mark_price,
    });

    const maxLeverage = item.leverage || 1;

    const imr = accountPerp.IMR({
      maxLeverage,
      baseIMR,
      IMR_Factor: account?.imr_factor[item.symbol] ?? 0,
      positionNotional: notional,
      ordersNotional: 0,
      IMR_factor_power: 4 / 5,
    });

    const unrealPnlROI = positionsPerp.unrealizedPnLROI({
      positionQty: item.position_qty,
      openPrice: item.average_open_price,
      IMR: imr,
      unrealizedPnL: unrealPnl,
    });

    let unrealPnl_index = 0;
    let unrealPnlROI_index = 0;
    if (item.index_price) {
      unrealPnl_index = positionsPerp.unrealizedPnL({
        qty: item.position_qty,
        openPrice: item?.average_open_price,
        markPrice: item.index_price,
      });
      unrealPnlROI_index = positionsPerp.unrealizedPnLROI({
        positionQty: item.position_qty,
        openPrice: item.average_open_price,
        IMR: imr,
        unrealizedPnL: unrealPnl_index,
      });
    }

    const filteredTPSLOrders = tpslOrders.filter(
      (tpslOrder) => tpslOrder.account_id === item.account_id,
    );

    const tpsl = formatTPSL(filteredTPSLOrders, item.symbol, item.margin_mode);

    return {
      ...item,
      ...tpsl,
      mmr: MMR,
      mm: mm,
      notional: notional,
      unrealized_pnl: unrealPnl,
      unrealized_pnl_ROI: unrealPnlROI,
      unrealized_pnl_ROI_index: unrealPnlROI_index,
    } as API.PositionTPSLExt;
  });
};

function formatTPSL(
  tpslOrders: API.AlgoOrder[],
  symbol: string,
  marginMode?: MarginMode,
) {
  /**
   * Merge TP/SL order data into position rows
   * Each position gets two TP/SL categories:
   * - full_tp_sl: Orders that close the entire position
   * - partial_tp_sl: Orders that close only part of the position
   * Only processes when TP/SL orders exist to optimize performance
   */
  if (Array.isArray(tpslOrders) && tpslOrders.length) {
    // Find TP/SL orders matching this position's symbol
    const { fullPositionOrder, partialPositionOrders } =
      findPositionTPSLFromOrders(tpslOrders, symbol, marginMode);

    // Extract TP/SL prices from full position order
    const full_tp_sl = fullPositionOrder
      ? findTPSLFromOrder(fullPositionOrder)
      : undefined;

    // Extract first partial position order (UI typically shows one at a time)
    const partialPossitionOrder =
      partialPositionOrders && partialPositionOrders.length
        ? partialPositionOrders[0]
        : undefined;
    const partial_tp_sl = partialPossitionOrder
      ? findTPSLFromOrder(partialPossitionOrder)
      : undefined;

    return {
      full_tp_sl: {
        tp_trigger_price: full_tp_sl?.tp_trigger_price,
        sl_trigger_price: full_tp_sl?.sl_trigger_price,
        algo_order: fullPositionOrder,
      },
      partial_tp_sl: {
        order_num: partialPositionOrders?.length ?? 0,
        tp_trigger_price: partial_tp_sl?.tp_trigger_price,
        sl_trigger_price: partial_tp_sl?.sl_trigger_price,
        algo_order: partialPossitionOrder,
      },
    };
  }
}
