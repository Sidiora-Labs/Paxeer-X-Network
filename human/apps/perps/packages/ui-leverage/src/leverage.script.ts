import { useCallback, useMemo, useState } from "react";
import { useLeverage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { SliderMarks, toast } from "@paxeer-network/ui";

type UseLeverageScriptOptions = {
  close?: () => void;
};

export type LeverageScriptReturns = ReturnType<typeof useLeverageScript>;

export const useLeverageScript = (options?: UseLeverageScriptOptions) => {
  const [showSliderTip, setShowSliderTip] = useState(false);
  const { t } = useTranslation();

  const { curLeverage, maxLeverage, isLoading, leverageLevers, update } =
    useLeverage();

  const marks = useMemo<SliderMarks>(() => {
    return leverageLevers.map((e) => ({
      label: `${e}x`,
      value: e,
    }));
  }, [leverageLevers]);

  const [leverage, setLeverage] = useState<number>(curLeverage ?? 0);

  const step = 100 / ((marks?.length || 0) - 1);

  const onLeverageChange = (leverage: number) => {
    setLeverage(leverage);
  };

  const onLeverageIncrease: React.MouseEventHandler<SVGSVGElement> = () => {
    setLeverage((prev) => prev + 1);
  };

  const onLeverageReduce: React.MouseEventHandler<SVGSVGElement> = () => {
    setLeverage((prev) => prev - 1);
  };

  const onInputChange = useCallback<React.ChangeEventHandler<HTMLInputElement>>(
    (e) => {
      const parsed = Number.parseInt(e.target.value);
      const value = Number.isNaN(parsed) ? "" : parsed;
      setLeverage(value as number);
    },
    [maxLeverage],
  );

  const onSave = async () => {
    try {
      update({ leverage }).then(
        () => {
          options?.close?.();
          toast.success(t("leverage.updated"));
        },
        (err: Error) => {
          toast.error(err.message);
        },
      );
    } catch (err) {
      console.log("update leverage error", err);
    }
  };

  const isReduceDisabled = leverage <= 1;
  const isIncreaseDisabled = leverage >= maxLeverage;
  const disabled = !leverage || leverage < 1 || leverage > maxLeverage;

  const toggles = useMemo(() => {
    return [5, 10, 20, 50, 100].filter((e) => e <= maxLeverage);
  }, [maxLeverage]);

  return {
    leverageLevers,
    currentLeverage: curLeverage,
    value: leverage,
    marks,
    onLeverageChange,
    onLeverageIncrease,
    onLeverageReduce,
    onInputChange,
    isReduceDisabled,
    isIncreaseDisabled,
    disabled,
    step,
    onCancel: options?.close,
    onSave,
    isLoading: isLoading,
    showSliderTip,
    setShowSliderTip,
    maxLeverage,
    toggles,
  };
};
