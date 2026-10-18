import { useEffect, useMemo, useRef, useState } from "react";
import {
  useAccount,
  useEventEmitter,
  useLocalStorage,
  useOrderStream,
  usePositionStream,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import {
  AccountStatusEnum,
  ORDER_ENTRY_EST_LIQ_PRICE_CHANGE,
  OrderEntryEstLiqPriceChangePayload,
  MarginMode,
  OrderStatus,
} from "@paxeer-network/types";
import {
  matchesTPSLPosition,
  withTPSLProvenance,
} from "@paxeer-network/utils";
import { DisplayControlSettingInterface } from "../../type";
import { Renderer } from "../renderer/renderer";
import { AlgoType } from "../type";

export default function useCreateRenderer(
  symbol: string,
  displayControlSetting?: DisplayControlSettingInterface,
  marginMode?: MarginMode,
) {
  const [renderer, setRenderer] = useState<Renderer>();
  const rendererRef = useRef<Renderer>();
  const { state } = useAccount();
  const [unPnlPriceBasis] = useLocalStorage("unPnlPriceBasis", "markPrice");
  const [{ rows }, positionsInfo] = usePositionStream(symbol, {
    calcMode: unPnlPriceBasis,
  });

  const positions = useMemo(() => {
    if (!rows?.length) return [];
    // return rows;
    return rows.filter((item) => item.symbol === symbol);
  }, [rows, symbol, marginMode]);
  const [pendingOrders] = useOrderStream({
    status: OrderStatus.INCOMPLETE,
    symbol: symbol,
  });

  const config = useSymbolsInfo();
  const symbolInfo = config?.[symbol];
  const quote_dp = symbolInfo("quote_dp");

  const [fillOrders] = useOrderStream({
    symbol: symbol,
    status: OrderStatus.FILLED,
    size: 500,
  });

  const ee = useEventEmitter();
  const [estimatedLiqPrice, setEstimatedLiqPrice] = useState<number | null>(
    null,
  );

  const createRenderer = useRef(
    (instance: any, host: any, broker: any, container: HTMLDivElement) => {
      if (rendererRef.current) {
        rendererRef.current.remove();
      }
      rendererRef.current = new Renderer(instance, host, broker);
      setRenderer(rendererRef.current);
    },
  );
  const removeRenderer = useRef(() => {
    rendererRef.current?.remove();
    rendererRef.current = undefined;
  });

  useEffect(() => {
    if (
      state.status < AccountStatusEnum.EnableTrading &&
      state.status !== AccountStatusEnum.EnableTradingWithoutConnected
    ) {
      renderer?.renderPositions([]);
      return;
    }

    if (!displayControlSetting || !displayControlSetting.position) {
      renderer?.renderPositions([]);
      return;
    }
    const positionList = positions.map((item) => {
      return {
        symbol: item.symbol,
        open: item.average_open_price,
        balance: item.position_qty,
        leverage: item.leverage,
        closablePosition: 9999,
        // @ts-ignore
        unrealPnl: item.unrealized_pnl ?? 0,
        interest: 0,
        unrealPnlDecimal: 2,
        basePriceDecimal: 4,
        marginMode: item.margin_mode,
        margin_mode: item.margin_mode,
        markPrice: item.mark_price,
      };
    });
    renderer?.renderPositions(positionList);
  }, [renderer, positions, symbol, displayControlSetting, state]);

  /** Subscribe to Order Entry estimated liq. price for the single liquidation line. */
  useEffect(() => {
    const handler = (payload: OrderEntryEstLiqPriceChangePayload) => {
      if (payload.symbol === symbol) {
        // Trust OrderEntry side to manage active window and timers; we just mirror estLiqPrice.
        setEstimatedLiqPrice(
          payload.isUserActive !== false ? payload.estLiqPrice : null,
        );
      }
    };
    ee.on(ORDER_ENTRY_EST_LIQ_PRICE_CHANGE, handler);
    return () => {
      ee.off(ORDER_ENTRY_EST_LIQ_PRICE_CHANGE, handler);
    };
  }, [ee, symbol]);

  /** Drive liquidation lines from position liq. prices + estimated liq. price (from event). */
  useEffect(() => {
    if (!renderer || !displayControlSetting?.position) return;
    const positionLiqItems = (positions ?? [])
      .filter((p) => p.symbol === symbol)
      .flatMap((p) => {
        const price = (p as { est_liq_price?: number | null }).est_liq_price;
        const leverage = (p as { leverage?: number | null }).leverage;
        if (price == null || !Number.isFinite(price)) {
          return [];
        }
        return [
          {
            price,
            marginMode: (p as { margin_mode?: "ISOLATED" | "CROSS" })
              .margin_mode,
            leverage: leverage ?? undefined,
          },
        ];
      });

    const effectiveEstimatedLiqPrice =
      estimatedLiqPrice != null && Number.isFinite(estimatedLiqPrice)
        ? estimatedLiqPrice
        : null;

    // Rendering rule:
    // - When effectiveEstimatedLiqPrice exists: render estimated liq. price line (always, regardless of liquidationPrice setting).
    // - When no estimated but positionLiqItems exists: render all position liq. price lines ONLY if liquidationPrice setting is enabled.
    // - When neither exists: remove line.
    const showPositionLiqItems =
      displayControlSetting?.liquidationPrice !== false ? positionLiqItems : [];

    renderer.renderLiquidationLine({
      positionLiqItems: showPositionLiqItems,
      estimatedLiqPrice: effectiveEstimatedLiqPrice,
    });
  }, [
    renderer,
    positions,
    symbol,
    estimatedLiqPrice,
    displayControlSetting?.position,
    displayControlSetting?.liquidationPrice,
  ]);

  useEffect(() => {
    if (!displayControlSetting || !displayControlSetting.buySell) {
      renderer?.renderFilledOrders([], 6);
      return;
    }
    const currentSymbolFillOrders = fillOrders?.filter(
      (item) => item.symbol === symbol,
    );
    renderer?.renderFilledOrders(currentSymbolFillOrders ?? [], quote_dp ?? 6);
  }, [renderer, fillOrders, symbol, quote_dp, displayControlSetting]);

  useEffect(() => {
    let tpslOrder: any = [];
    let positionTpsl: any = [];
    let limitOrder: any = [];
    let stopOrder: any = [];
    let bracketOrder: any = [];
    let trailingStopOrder: any = [];

    if (
      state.status < AccountStatusEnum.EnableTrading &&
      state.status !== AccountStatusEnum.EnableTradingWithoutConnected
    ) {
      renderer?.renderPendingOrders([]);
      return;
    }

    pendingOrders?.forEach((rawOrder) => {
      const order = withTPSLProvenance(rawOrder);
      const symbolPosition = positions.find((item) =>
        matchesTPSLPosition(order, item),
      );
      if (symbol !== order.symbol) {
        return;
      }
      if (!order.algo_order_id) {
        limitOrder.push(order);
      } else if (order.algo_order_id) {
        if (order.algo_type === AlgoType.POSITIONAL_TP_SL) {
          for (const child_order of order.child_orders) {
            child_order.root_algo_order_algo_type = order.algo_type;
            if (
              child_order.trigger_price &&
              child_order.status !== OrderStatus.FILLED
            ) {
              positionTpsl.push(child_order);
            }
          }
        } else if (order.algo_type === AlgoType.TP_SL) {
          for (const child_order of order.child_orders) {
            child_order.root_algo_order_algo_type = order.algo_type;
            child_order.position_qty = symbolPosition?.position_qty;
            if (
              child_order.trigger_price &&
              child_order.status !== OrderStatus.FILLED
            ) {
              tpslOrder.push(child_order);
            }
          }
        } else if (
          order.algo_type === AlgoType.STOP_LOSS ||
          order.algo_type === AlgoType.TAKE_PROFIT
        ) {
          stopOrder.push(order);
        } else if (order.algo_type === AlgoType.BRACKET) {
          bracketOrder.push(order);
          for (const parent of order.child_orders ?? []) {
            for (const child of parent.child_orders ?? []) {
              if (
                !child.trigger_price ||
                [
                  OrderStatus.FILLED,
                  OrderStatus.CANCELLED,
                  OrderStatus.REJECTED,
                ].includes(child.algo_status)
              )
                continue;
              const lineOrder = {
                ...child,
                root_algo_order_algo_type: parent.algo_type,
                position_qty: symbolPosition?.position_qty,
              };
              if (parent.algo_type === AlgoType.POSITIONAL_TP_SL)
                positionTpsl.push(lineOrder);
              else tpslOrder.push(lineOrder);
            }
          }
        } else if (order.algo_type === AlgoType.TRAILING_STOP) {
          if (order.is_activated && order.extreme_price) {
            trailingStopOrder.push(order);
          }
        }
      }
    });
    if (displayControlSetting) {
      if (!displayControlSetting.positionTpsl) {
        positionTpsl = [];
      }
      if (!displayControlSetting.tpsl) {
        tpslOrder = [];
      }
      if (!displayControlSetting.limitOrders) {
        limitOrder = [];
        bracketOrder = [];
      }
      if (!displayControlSetting.stopOrders) {
        stopOrder = [];
      }
      if (!displayControlSetting.trailingStop) {
        trailingStopOrder = [];
      }
    }

    renderer?.renderPendingOrders(
      tpslOrder
        .concat(positionTpsl)
        .concat(limitOrder)
        .concat(stopOrder)
        .concat(bracketOrder)
        .concat(trailingStopOrder),
    );
  }, [
    renderer,
    pendingOrders,
    symbol,
    displayControlSetting,
    positions,
    state.status,
  ]);

  return [createRenderer.current, removeRenderer.current] as const;
}
