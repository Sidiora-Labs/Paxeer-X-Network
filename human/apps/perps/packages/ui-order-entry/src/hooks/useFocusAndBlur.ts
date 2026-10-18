import { useRef, FocusEvent } from "react";
import { OrderEntryReturn, useMemoizedFn, utils } from "@paxeer-network/hooks";
import { OrderType } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { InputType } from "../types";

type FocusAndBlurProps = {
  base_tick: number;
  order_type?: OrderType;
  order_quantity?: string;
  setValue: OrderEntryReturn["setValue"];
};

export function useFocusAndBlur(props: FocusAndBlurProps) {
  const { base_tick, order_type, order_quantity, setValue } = props;
  const currentFocusInput = useRef<InputType>(InputType.NONE);
  const lastScaledOrderPriceInput = useRef<InputType>(InputType.END_PRICE);
  const lastQuantityInputType = useRef<InputType>(InputType.NONE);

  const formatQty = () => {
    if (
      base_tick < 1 ||
      // scaled order should not format quantity, because it is total quantity
      order_type === OrderType.SCALED ||
      !order_quantity
    ) {
      return;
    }

    // TODO: use this to format quantity instead of utils.formatNumber, need time to test
    // const formatQty = new Decimal(formattedOrder.order_quantity)
    //   .todp(0, Decimal.ROUND_DOWN)
    //   .div(symbolInfo.base_tick)
    //   .toString();

    const quantity = utils.formatNumber(
      order_quantity,
      new Decimal(base_tick || "0").toNumber(),
    );

    setValue("order_quantity", quantity, {
      shouldUpdateLastChangedField: false,
    });
  };

  const onFocus = (type: InputType) => (_: FocusEvent) => {
    currentFocusInput.current = type;

    // set last scaled order price input
    if (
      [InputType.START_PRICE, InputType.END_PRICE].includes(
        currentFocusInput.current!,
      )
    ) {
      lastScaledOrderPriceInput.current = type;
    }

    if (
      [InputType.QUANTITY, InputType.TOTAL, InputType.MARGIN].includes(type)
    ) {
      lastQuantityInputType.current = type;
    }
  };

  const onBlur = (type: InputType) => (_: FocusEvent) => {
    setTimeout(() => {
      if (currentFocusInput.current !== type) {
        return;
      }
      currentFocusInput.current = InputType.NONE;
    }, 300);

    if (type === InputType.QUANTITY) {
      formatQty();
    }
  };

  return {
    currentFocusInput,
    lastScaledOrderPriceInput,
    lastQuantityInputType,
    onFocus: useMemoizedFn(onFocus),
    onBlur: useMemoizedFn(onBlur),
  };
}
