import { API, PositionType, SDKError } from "@paxeer-network/types";
import { useTaskProfitAndStopLossInternal } from "./useTPSL";

export const useTPSLOrder = (
  /**
   * Position that needs to set take profit and stop loss
   */
  position: Partial<API.PositionTPSLExt> &
    Pick<API.PositionTPSLExt, "symbol" | "average_open_price" | "position_qty">,
  options?: {
    /**
     * You can set the default value for the take profit and stop loss order,
     * it is usually used when editing order
     */
    defaultOrder?: API.AlgoOrder;
    // tpslEnable?: {
    //   tp_enable?: boolean;
    //   sl_enable?: boolean;
    // };
    isEditing?: boolean;
    positionType?: PositionType;
  },
): ReturnType<typeof useTaskProfitAndStopLossInternal> => {
  if (!position) {
    throw new SDKError("Position is required");
  }

  if (!position.symbol) {
    throw new SDKError("Symbol is required");
  }

  if (typeof position.average_open_price === "undefined") {
    throw new SDKError("Average open price is required");
  }

  if (typeof position.position_qty === "undefined") {
    throw new SDKError("Position quantity is required");
  }

  // const { data: markPrice } = useMarkPrice(position.symbol);

  const result = useTaskProfitAndStopLossInternal(position, options);

  return result;
};

export type { ComputedAlgoOrder } from "./useTPSL";
