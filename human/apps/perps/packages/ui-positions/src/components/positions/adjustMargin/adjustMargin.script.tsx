import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import usePositionMargin from "./hooks/usePositionMargin";

export type AdjustMarginTab = "add" | "reduce";

type AmountInputSource = "input" | "slider";

export interface AdjustMarginScriptProps {
  position: API.PositionTPSLExt;
  symbol: string;
  close: () => void;
}

export interface AdjustMarginState {
  symbol: string;
  tab: AdjustMarginTab;
  inputValue: string;
  sliderValue: number;
  maxAmount: number;
  currentMargin: number;
  liquidationPrice: number | null;
  effectiveLeverage: number;
  isLoading: boolean;
  isAdd: boolean;
  canConfirm: boolean;
  onTabChange: (tab: AdjustMarginTab) => void;
  onInputChange: (value: string) => void;
  onSliderChange: (value: number) => void;
  onConfirm: () => Promise<void>;
  close: () => void;
}

export const useAdjustMarginScript = (
  props: AdjustMarginScriptProps,
): AdjustMarginState => {
  const { position, symbol, close } = props;
  const { t } = useTranslation();

  const [tab, setTab] = useState<AdjustMarginTab>("add");
  const [inputValue, setInputValue] = useState("");
  const [sliderValue, setSliderValue] = useState(0);
  const lastInputSource = useRef<AmountInputSource>("input");
  const selectedSliderValue = useRef(0);

  const [updateMargin, { isMutating: isLoading }] = useMutation(
    "/v1/position_margin",
    "POST",
  );

  const isAdd = useMemo(() => {
    return tab === "add";
  }, [tab]);

  const currentMargin = position.margin ?? 0;

  const finalMargin = useMemo(() => {
    const delta = new Decimal(inputValue || 0);
    if (tab === "add") return currentMargin + delta.toNumber();
    return currentMargin - delta.toNumber();
  }, [currentMargin, inputValue, tab]);

  const { maxAmount, liquidationPrice, effectiveLeverage } = usePositionMargin(
    symbol,
    isAdd,
    currentMargin,
    finalMargin,
  );

  const previousMaxAmount = useRef(maxAmount);

  useEffect(() => {
    if (Object.is(previousMaxAmount.current, maxAmount)) return;
    previousMaxAmount.current = maxAmount;

    if (maxAmount === null || maxAmount <= 0) {
      setInputValue("");
      setSliderValue(0);
      lastInputSource.current = "input";
      selectedSliderValue.current = 0;
      return;
    }

    if (lastInputSource.current === "slider") {
      const nextValue = new Decimal(maxAmount)
        .mul(selectedSliderValue.current)
        .div(100);
      setInputValue(nextValue.toFixed(2, Decimal.ROUND_DOWN));
      return;
    }

    if (!inputValue) {
      setSliderValue(0);
      return;
    }

    const inputAmount = new Decimal(inputValue);
    const nextValue = inputAmount.gt(maxAmount)
      ? new Decimal(maxAmount).toFixed(2, Decimal.ROUND_DOWN)
      : inputValue;
    const nextSliderValue = new Decimal(nextValue)
      .div(maxAmount)
      .mul(100)
      .toNumber();

    setInputValue(nextValue);
    setSliderValue(Math.min(100, Math.max(0, nextSliderValue)));
  }, [inputValue, maxAmount]);

  const onInputChange = useCallback(
    (value: string) => {
      let finalValue = value;

      if (maxAmount !== null && value) {
        const inputDecimal = new Decimal(value);
        if (inputDecimal.gt(maxAmount)) {
          finalValue = new Decimal(maxAmount).toFixed(2, Decimal.ROUND_DOWN);
        }
      }

      lastInputSource.current = "input";
      setInputValue(finalValue);

      const nextSliderValue =
        finalValue && maxAmount
          ? new Decimal(finalValue).div(maxAmount).mul(100).toNumber()
          : 0;
      setSliderValue(Math.min(100, Math.max(0, nextSliderValue)));
    },
    [maxAmount],
  );

  const onSliderChange = useCallback(
    (value: number) => {
      if (!maxAmount) return;

      lastInputSource.current = "slider";
      selectedSliderValue.current = value;
      setSliderValue(value);

      const nextValue = new Decimal(maxAmount).mul(value).div(100);
      setInputValue(nextValue.toFixed(2, Decimal.ROUND_DOWN));
    },
    [maxAmount],
  );

  const onTabChange = useCallback((nextTab: AdjustMarginTab) => {
    setTab(nextTab);
    setInputValue("");
    setSliderValue(0);
    lastInputSource.current = "input";
    selectedSliderValue.current = 0;
  }, []);

  const canConfirm = useMemo(() => {
    if (!inputValue || maxAmount === null || maxAmount <= 0) return false;
    const value = new Decimal(inputValue);
    return value.isPositive() && value.lte(maxAmount);
  }, [inputValue, maxAmount]);

  const onConfirm = useCallback(async () => {
    if (!inputValue || new Decimal(inputValue).isZero()) return;

    // Validate if input value exceeds maxAmount
    if (maxAmount !== null) {
      const inputDecimal = new Decimal(inputValue);
      if (inputDecimal.gt(maxAmount)) {
        toast.error(t("positions.adjustMargin.marginCannotMoreThanMax"));
        return;
      }
    }

    try {
      const payload = {
        symbol,
        amount: new Decimal(inputValue).toString(),
        type: tab === "add" ? "ADD" : "REDUCE",
      };
      const response = await updateMargin(payload);

      if (!response?.success) {
        toast.error(response?.message || t("positions.adjustMargin.failed"));
        return;
      }

      toast.success(t("positions.adjustMargin.success"));
      close();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : undefined;
      toast.error(message || t("positions.adjustMargin.failed"));
    }
  }, [close, inputValue, symbol, t, tab, updateMargin, maxAmount]);

  return {
    symbol,
    tab,
    inputValue,
    sliderValue,
    maxAmount: maxAmount ?? 0,
    currentMargin: currentMargin,
    liquidationPrice,
    effectiveLeverage: effectiveLeverage ?? 0,
    isLoading,
    isAdd,
    canConfirm,
    onTabChange,
    onInputChange,
    onSliderChange,
    onConfirm,
    close,
  };
};
