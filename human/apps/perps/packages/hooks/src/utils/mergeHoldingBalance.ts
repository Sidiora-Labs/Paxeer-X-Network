import type { API } from "@paxeer-network/types";

type BalanceUpdate = Partial<API.Holding> & {
  pendingShort?: number;
  pendingShortQty?: number;
  isolatedMargin?: number;
  isolatedOrderFrozen?: number;
};

/** Normalize REST snake_case and WebSocket camelCase balance fields. */
export const mergeHoldingBalance = (
  holding: API.Holding,
  update: BalanceUpdate,
): API.Holding => {
  return {
    ...holding,
    holding: update.holding ?? holding.holding,
    frozen: update.frozen ?? holding.frozen,
    pending_short:
      update.pending_short ??
      update.pendingShort ??
      update.pendingShortQty ??
      holding.pending_short,
    isolated_margin:
      update.isolated_margin ??
      update.isolatedMargin ??
      holding.isolated_margin,
    isolated_order_frozen:
      update.isolated_order_frozen ??
      update.isolatedOrderFrozen ??
      holding.isolated_order_frozen,
  };
};
