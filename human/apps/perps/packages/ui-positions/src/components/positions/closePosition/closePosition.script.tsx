import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocalStorage, utils } from "@paxeer-network/hooks";
import { useOrderEntryFormErrorMsg } from "@paxeer-network/react-app";
import { OrderType } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { useSymbolContext } from "../../../provider/symbolContext";
import { usePositionsRowContext } from "../positionsRowContext";

export type ClosePositionScriptProps = {
  type?: OrderType.MARKET | OrderType.LIMIT;
};

export type ClosePositionScriptReturn = ReturnType<
  typeof useClosePositionScript
>;

export const useClosePositionScript = (props: ClosePositionScriptProps) => {
  const [sheetOpen, setSheetOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [sliderValue, setSliderValue] = useState(100);
  const quantityInputRef = useRef<HTMLInputElement>(null);
  const [orderConfirm] = useLocalStorage("paxeerNetwork_order_confirm", true);

  const {
    position,
    onSubmit,
    price,
    quantity,
    closeOrderData,
    submitting,
    updatePriceChange,
    updateOrderType,
    updateQuantity,
    errors,
    baseTick,
    type,
  } = usePositionsRowContext();
  const { quote_dp, base_dp, base, quote } = useSymbolContext();
  const { getErrorMsg } = useOrderEntryFormErrorMsg(errors);

  // buy position quantity is positive, sell position quantity is negative
  const isBuy = position.position_qty > 0;

  // we need to use Math.abs to get the absolute value of the position quantity
  const maxQty = Math.abs(position.position_qty);

  // close position type: limit or market
  const closeType = props.type || type;

  const isMarketClose = closeType === OrderType.MARKET;

  const isEntirePosition = maxQty?.toString() === quantity.toString();

  useEffect(() => {
    // mobile sheet open, update order type
    if (sheetOpen) {
      updateOrderType(props.type as OrderType);
    }
  }, [props.type, sheetOpen]);

  // reset state when sheet is closed
  // useEffect(() => {
  //   if (!sheetOpen || !popoverOpen) {
  //     // set default slider value to 100 when table edit remove
  //     updateQuantity(maxQty?.toString());
  //   }
  // }, [sheetOpen, popoverOpen, maxQty]);

  const { priceErrorMsg, quantityErrorMsg } = useMemo(() => {
    return {
      priceErrorMsg: getErrorMsg("order_price"),
      quantityErrorMsg: getErrorMsg("order_quantity"),
    };
    // getErrorMsg is changed when errors is changed
  }, [errors]);

  const disabled = !!(priceErrorMsg || quantityErrorMsg);

  const formatQuantityToBaseTick = useCallback(
    (value: string) => {
      if (baseTick && baseTick > 0) {
        // format quantity to baseTick
        const formatQty = utils.formatNumber(value, baseTick) ?? value;
        updateQuantity(formatQty);
      }
    },
    [baseTick, updateQuantity],
  );

  const onSliderValueChange = useCallback(
    (value: number) => {
      setSliderValue(value);

      // transform slider value to quantity
      const qty = new Decimal(value)
        .div(100)
        .mul(maxQty)
        .toFixed(base_dp, Decimal.ROUND_DOWN);

      formatQuantityToBaseTick(qty);
    },
    [maxQty, base_dp, formatQuantityToBaseTick],
  );

  useEffect(() => {
    const qty = Math.min(Number(quantity || 0), maxQty);
    // transform quantity to slider value
    const slider = new Decimal(qty)
      .div(maxQty)
      .mul(100)
      .toDecimalPlaces(2, Decimal.ROUND_DOWN)
      .toNumber();

    setSliderValue(slider);
  }, [quantity]);

  const onMax = useCallback(() => {
    updateQuantity(maxQty?.toString());
  }, [maxQty, updateQuantity]);

  const onConfirm = useCallback(() => {
    return onSubmit().then((res) => {
      setSheetOpen(false);
      setDialogOpen(false);
      setPopoverOpen(false);
    });
  }, [onSubmit]);

  const onDoubleConfirm = useCallback(() => {
    if (!orderConfirm) {
      return onConfirm();
    }
    setDialogOpen(true);
    return Promise.resolve();
  }, [onConfirm, orderConfirm]);

  const onCloseSheet = useCallback(() => {
    setSheetOpen(false);
  }, []);

  const onCloseDialog = useCallback(() => {
    setDialogOpen(false);
  }, []);

  const onClosePopover = useCallback(() => {
    setPopoverOpen(false);
  }, []);

  // when click entire position button, set quantity to 0 and focus on input
  const onEntirePosition = useCallback(() => {
    updateQuantity("0");
    quantityInputRef.current?.focus();

    setTimeout(() => {
      quantityInputRef.current?.setSelectionRange(0, 1);
    }, 0);
  }, [updateQuantity]);

  return {
    position,
    closeOrderData,
    quote_dp,
    base_dp,
    base,
    quote,
    price,
    quantity,
    submitting,

    onConfirm,
    onDoubleConfirm,
    updatePriceChange,
    updateQuantity,
    formatQuantityToBaseTick,
    updateOrderType,

    sliderValue,
    onSliderValueChange,

    priceErrorMsg,
    quantityErrorMsg,
    disabled,
    isMarketClose,

    maxQty,
    onMax,
    isBuy,
    isEntirePosition,
    type: closeType,
    quantityInputRef,
    onEntirePosition,

    sheetOpen,
    onCloseSheet,
    setSheetOpen,

    dialogOpen,
    setDialogOpen,
    onCloseDialog,

    popoverOpen,
    setPopoverOpen,
    onClosePopover,
  };
};
