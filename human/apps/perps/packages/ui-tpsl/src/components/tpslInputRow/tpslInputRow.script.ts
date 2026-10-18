import { useMemo } from "react";
import {
  OrderValidationResult,
  useLeverageBySymbol,
} from "@paxeer-network/hooks";
import { order } from "@paxeer-network/perp";
import {
  MarginMode,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";

export type Props = {
  disableOrderTypeSelector?: boolean;
  disableTriggerEditing?: boolean;
  type: "tp" | "sl";
  quote_dp: number;
  positionType: PositionType;
  errors: OrderValidationResult | null;
  values: {
    // enable: boolean;
    trigger_price: string | undefined;
    PnL: string | undefined;
    Offset: string | undefined;
    "Offset%": string | undefined;
    /** Mark-price based offset (USDC), maps to `tp_offset_from_mark` / `sl_offset_from_mark` on order. */
    OffsetFromMark: string | undefined;
    /** Mark-price based offset (%), maps to `tp_offset_percentage_from_mark` / `sl_offset_percentage_from_mark`. */
    PercentageFromMark: string | undefined;
    ROI?: string | undefined;
    order_price: string | undefined;
    order_type: OrderType;
  };
  onChange: (key: string, value: string | boolean | number) => void;
  rootOrderPrice: string | undefined;
  symbol: string;
  disableEnableCheckbox?: boolean;
  symbolLeverage?: number;
  side: OrderSide;
  inputWarnNode?: React.ReactNode;
  marginMode?: MarginMode;
};
export const useTPSLInputRowScript = (props: Props) => {
  const { values, side, type, rootOrderPrice } = props;
  // if symbolLeverage is not provided, get it from useLeverageBySymbol
  const symbolLeverage = useLeverageBySymbol(
    props.symbolLeverage ? undefined : props.symbol,
    props.marginMode,
  );

  const leverage = props.symbolLeverage || symbolLeverage;

  const roi = useMemo(() => {
    if (!leverage || !rootOrderPrice || Number(rootOrderPrice) === 0) {
      return null;
    }

    let closePrice: string | undefined;
    if (values.order_type === OrderType.MARKET) {
      closePrice = values.trigger_price;
    } else if (values.order_type === OrderType.LIMIT) {
      closePrice = values.order_price;
    }
    if (!closePrice) {
      return null;
    }

    try {
      const roi = order.tpslROI({
        side,
        type,
        closePrice: Number(closePrice),
        orderPrice: Number(rootOrderPrice),
        leverage,
      });
      return roi * 100;
    } catch (error) {
      console.error("error", error);
      return null;
    }
  }, [values, leverage, rootOrderPrice, type, side]);

  return {
    ...props,
    roi,
  };
};
