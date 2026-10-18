import { useCallback, useMemo } from "react";
import {
  usePositionStream,
  useSubAccountMutation,
  useTrack,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  API,
  MarginMode,
  OrderSide,
  OrderType,
  TrackerEventName,
} from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";

/** Limit: 1 request per 1 second. Max 10 orders per batch. @see https://paxeer-network.network/docs/build-on-omnichain/evm-api/restful-api/private/batch-create-order */
const MAX_BATCH_ORDER_SIZE = 10;

/** Delay between batch requests (ms). API limit: 1 request per 1 second. */
const BATCH_RATE_LIMIT_MS = 1100;

export enum CloseType {
  ALL = "ALL",
  PROFIT = "PROFIT",
  LOSS = "LOSS",
}

function getPositionsByCloseType(
  positions: API.PositionTPSLExt[],
  closeType: CloseType,
): API.PositionTPSLExt[] {
  if (closeType === CloseType.ALL) return positions;
  if (closeType === CloseType.PROFIT) {
    return positions.filter((p) => (p.unrealized_pnl ?? 0) > 0);
  }
  if (closeType === CloseType.LOSS) {
    return positions.filter((p) => (p.unrealized_pnl ?? 0) < 0);
  }
  return positions;
}

export interface UseCloseAllPositionsScriptOptions {
  symbol?: string;
  onSuccess?: () => void;
}

export const useCloseAllPositionsScript = (
  options?: UseCloseAllPositionsScriptOptions,
) => {
  const { symbol, onSuccess } = options || {};
  const { t } = useTranslation();
  const { tracking } = useTrack();

  // Get all positions data
  const [positionsData] = usePositionStream(symbol);

  // Get open positions (positions with non-zero quantity)
  const openPositions = useMemo(() => {
    if (!positionsData?.rows) return [];
    return positionsData.rows.filter(
      (position: API.PositionTPSLExt) =>
        position.position_qty !== 0 && position.position_qty !== undefined,
    );
  }, [positionsData]);

  // Check if there are open positions
  const hasOpenPositions = useMemo(() => {
    return openPositions.length > 0;
  }, [openPositions]);

  const [doBatchCreateOrder, { isMutating }] = useSubAccountMutation(
    "/v1/batch-order",
    "POST",
  );

  // Build batch order body: one market reduce_only order per position
  const buildOrdersFromPositions = useCallback(
    (positions: API.PositionTPSLExt[]) =>
      positions.map((position: API.PositionTPSLExt) => {
        const side = position.position_qty > 0 ? OrderSide.SELL : OrderSide.BUY;
        const quantity = Math.abs(position.position_qty);
        return {
          symbol: position.symbol,
          order_type: OrderType.MARKET,
          side,
          order_quantity: quantity,
          reduce_only: true,
          native_position_id:(position as API.PositionTPSLExt & {position_id?:string|number}).position_id,
          native_close_all:true,
          margin_mode: position.margin_mode,
        };
      }),
    [],
  );

  // Close positions via batch-order API (accepts list to support ALL/PROFIT/LOSS filtering)
  const closePositions = useCallback(
    async (positionsToClose: API.PositionTPSLExt[]) => {
      if (positionsToClose.length === 0) return false;

      try {
        const orders = buildOrdersFromPositions(positionsToClose);

        for (let i = 0; i < orders.length; i += MAX_BATCH_ORDER_SIZE) {
          const batch = orders.slice(i, i + MAX_BATCH_ORDER_SIZE);
          const result = await doBatchCreateOrder({ orders: batch });
          if (result?.error) {
            throw result.error;
          }
          // API limit: 1 request per 1 second; wait before next batch
          if (i + MAX_BATCH_ORDER_SIZE < orders.length) {
            await new Promise((resolve) =>
              setTimeout(resolve, BATCH_RATE_LIMIT_MS),
            );
          }
        }

        toast.success(t("positions.closeAll.success"));
        onSuccess?.();

        return true;
      } catch (error: any) {
        if (error?.message !== undefined) {
          toast.error(error.message);
        }
        return false;
      }
    },
    [doBatchCreateOrder, buildOrdersFromPositions, t, onSuccess],
  );

  const confirmAndCloseAll = useCallback(
    async (closeType: CloseType): Promise<boolean> => {
      const positionsToClose = getPositionsByCloseType(
        openPositions,
        closeType,
      );
      if (positionsToClose.length === 0) {
        if (closeType === CloseType.PROFIT || closeType === CloseType.LOSS) {
          const msg =
            closeType === CloseType.PROFIT
              ? t("positions.closeAll.noPositions.profit")
              : t("positions.closeAll.noPositions.loss");
          toast.error(msg);
          onSuccess?.();
          return true;
        }
        return false;
      }
      tracking(TrackerEventName.confirmCloseAllPositions, {
        positions_count: positionsToClose.length,
      });
      return closePositions(positionsToClose);
    },
    [openPositions, tracking, closePositions, t, onSuccess],
  );

  return {
    confirmAndCloseAll,
    hasOpenPositions,
    isClosing: isMutating,
    openPositionsCount: openPositions.length,
    symbol,
  };
};

export type CloseAllPositionsState = ReturnType<
  typeof useCloseAllPositionsScript
>;
