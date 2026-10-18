import { useCallback, useEffect, useMemo, useState } from "react";
import {
  useAccountInfo,
  useLocalStorage,
  useMarkPricesStream,
  usePortfolio,
  usePositionStream,
  useSymbolLeverage,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  account as accountPerp,
  positions as positionsPerp,
} from "@paxeer-network/perp";
import { MarginMode, OrderSide } from "@paxeer-network/types";
import {
  modal,
  SliderMarks,
  toast,
  useScreen,
  Text,
} from "@paxeer-network/ui";
import { zero } from "@paxeer-network/utils";

type UseLeverageScriptOptions = {
  close?: () => void;
};

export type SymbolLeverageScriptOptions = {
  symbol: string;
  side?: OrderSide;
  curLeverage: number;
  marginMode: MarginMode;
};

export type SymbolLeverageScriptReturns = ReturnType<
  typeof useSymbolLeverageScript
>;

export const useSymbolLeverageScript = (
  options: SymbolLeverageScriptOptions & UseLeverageScriptOptions,
) => {
  const { curLeverage = 1, symbol, side, marginMode } = options;
  const [showSliderTip, setShowSliderTip] = useState(false);
  // Local leverage value used by the input and slider; it tracks the in-flight user edits.
  // We seed it with curLeverage but intentionally do not sync further changes to avoid jumping while editing.
  const [leverage, setLeverage] = useState<number>(curLeverage);

  const { t } = useTranslation();

  const { isMobile } = useScreen();

  const {
    maxLeverage: originalMaxLeverage,
    update,
    isLoading,
  } = useSymbolLeverage(symbol);

  // Sync when external current leverage changes (e.g. symbol or margin mode changed)
  useEffect(() => {
    setLeverage(curLeverage);
  }, [curLeverage]);

  const maxLeverage = originalMaxLeverage;

  const {
    position,
    maxPositionNotional,
    maxPositionLeverage,
    overMaxPositionLeverage,
    overRequiredMargin,
  } = useCalc({ symbol: symbol!, leverage, maxLeverage, marginMode });

  const formattedLeverageLevers = useMemo(() => {
    return generateLeverageLeversForSelector(maxLeverage);
  }, [maxLeverage]);

  const leverageLevers = useMemo(() => {
    return generateLeverageLevers(maxLeverage);
  }, [maxLeverage]);

  const marks = useMemo<SliderMarks>(() => {
    return (
      leverageLevers.map((e) => ({
        label: `${e}x`,
        value: e,
      })) || []
    );
  }, [leverageLevers]);

  const step = useMemo(() => {
    return 100 / ((marks?.length || 0) - 1);
  }, [marks]);

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
    [],
  );

  const onInputBlur = useCallback<React.FocusEventHandler<HTMLInputElement>>(
    (e) => {
      const inputValue = e.target.value.trim();
      if (inputValue === "") {
        setLeverage(curLeverage);
      }
    },
    [curLeverage],
  );

  const onConfirmSave = async () => {
    try {
      update?.({ leverage, symbol, margin_mode: marginMode }).then(
        (res) => {
          if (res.success) {
            options?.close?.();
            toast.success(t("leverage.updated"));
          } else {
            toast.error(res.message);
          }
        },
        (err: Error) => {
          toast.error(err.message);
        },
      );
    } catch (err) {
      console.error("update leverage error", err);
    }
  };

  const onSave = async () => {
    modal.confirm({
      title: t("leverage.confirm"),
      content: <Text intensity={54}>{t("leverage.confirm.content")}</Text>,
      onOk: onConfirmSave,
      onCancel: () => {
        return Promise.resolve();
      },
    });
  };

  const isReduceDisabled = leverage <= 1;
  const isIncreaseDisabled = leverage >= maxLeverage;

  const isBuy = side
    ? side === OrderSide.BUY
    : position?.position_qty && position.position_qty > 0;

  const disabled =
    !leverage ||
    leverage < 1 ||
    leverage > maxLeverage ||
    overRequiredMargin ||
    overMaxPositionLeverage;

  return {
    leverageLevers,
    currentLeverage: curLeverage, // Keep the displayed leverage fixed until the user confirms the change.
    value: leverage, // Input and slider reflect the temporary value being edited.
    marks,
    onLeverageChange,
    onLeverageIncrease,
    onLeverageReduce,
    onInputChange,
    onInputBlur,
    isReduceDisabled,
    isIncreaseDisabled,
    disabled,
    step,
    onCancel: options?.close,
    onSave,
    isLoading,
    showSliderTip,
    setShowSliderTip,
    maxLeverage,
    toggles: formattedLeverageLevers,
    symbol,
    maxPositionNotional,
    maxPositionLeverage,
    overMaxPositionLeverage,
    overRequiredMargin,
    isBuy,
    isMobile,
    marginMode,
  };
};

// 5x: 1x, 2x, 3x, 4x, 5x
// 10x: 1x, 3x, 5x, 8x, 10x
// 20x: 1x, 5x, 10x, 15x, 20x
// 50x: 1x, 10x, 20x, 35x, 50x
// 100x: 1x, 20x, 50x, 75x, 100x
const generateLeverageLeversForSelector = (max: number) => {
  if (max === 10) {
    return [1, 3, 5, 8, 10];
  } else if (max === 50) {
    return [1, 10, 20, 35, 50];
  }

  const min = 1;
  const parts = 5;
  const step = (max - min) / (parts - 1);
  const result: number[] = [];
  for (let i = 0; i < parts; i++) {
    result.push(Math.floor(min + step * i));
  }
  return result;
};
/**
 * Generate evenly distributed marks
 * @param max Maximum leverage value
 * @returns Array of evenly distributed marks
 */
const generateEvenlyDistributedMarks = (max: number): number[] => {
  const result: number[] = [];

  // Check if divisible by 5
  if (max % 5 === 0) {
    // Divisible by 5, divide from 0 to max into 5 intervals (6 marks), 1x represents 0
    const step = max / 5;
    for (let i = 0; i < 6; i++) {
      const value = step * i;
      result.push(value === 0 ? 1 : value); // 0 displays as 1x, other values display normally
    }
  } else {
    // Not divisible by 5, use 25%, 50%, 75% strategy to select nearest integers
    result.push(1); // Always include 1x

    // Calculate values at 25%, 50%, 75% positions
    const quarter = max * 0.25;
    const half = max * 0.5;
    const threeQuarter = max * 0.75;

    // Select nearest integers
    const quarterRounded = Math.round(quarter);
    const halfRounded = Math.round(half);
    const threeQuarterRounded = Math.round(threeQuarter);

    // Add 25% position value (if greater than 1 and not equal to 50%)
    if (quarterRounded > 1 && quarterRounded !== halfRounded) {
      result.push(quarterRounded);
    }

    // Add 50% position value (if greater than 1)
    if (halfRounded > 1) {
      result.push(halfRounded);
    }

    // Add 75% position value (if greater than 50% and less than max)
    if (threeQuarterRounded > halfRounded && threeQuarterRounded < max) {
      result.push(threeQuarterRounded);
    }

    // Add maximum value (if greater than 1)
    if (max > 1) {
      result.push(max);
    }
  }

  return result;
};

// 5x: 1, 2, 3, 4, 5
// 10x: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10
// 20x: 1, 5, 10, 15, 20
// 50x: 1, 10, 20, 30, 40, 50
// 100x: 1, 20, 40, 60, 80, 100
const generateLeverageLevers = (max: number) => {
  switch (max) {
    case 10:
      return [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
    case 20:
      return [1, 5, 10, 15, 20];
    case 50:
      return [1, 10, 20, 30, 40, 50];
    case 100:
      return [1, 20, 40, 60, 80, 100];
  }

  // Fallback strategy: evenly distribute mark distances, treat 1x as 0
  const result: number[] = [];
  // Optimization
  if (max < 10) {
    // For 10x and below, divide into n equal parts, each with length 1
    for (let i = 1; i <= max; i++) {
      result.push(i);
    }
  } else {
    // Use unified even distribution strategy
    result.push(...generateEvenlyDistributedMarks(max));
  }

  return result;
};

function useCalc(inputs: {
  symbol: string;
  leverage: number;
  maxLeverage: number;
  marginMode: MarginMode;
}) {
  const { symbol, leverage, maxLeverage, marginMode } = inputs;

  const symbolsInfo = useSymbolsInfo();
  const { data: accountInfo } = useAccountInfo();
  const { data: markPrices } = useMarkPricesStream();
  const { totalCollateral } = usePortfolio();

  const [unPnlPriceBasis, setUnPnlPriceBasic] = useLocalStorage(
    "unPnlPriceBasis",
    "markPrice",
  );
  const [positions] = usePositionStream("all", {
    calcMode: unPnlPriceBasis,
    includedPendingOrder: true,
  });

  const position = useMemo(() => {
    if (symbol && positions?.rows?.length) {
      return positions.rows.find(
        (item) => item.symbol === symbol && item.margin_mode === marginMode,
      );
    }
  }, [positions, symbol, marginMode]);

  /** the highest allowable leverage. Block users from setting leverage above this limit. */
  const maxPositionLeverage = useMemo(() => {
    const IMRFactor = accountInfo?.imr_factor?.[symbol];
    const notional = position?.notional;
    // when user has existing position
    if (IMRFactor && notional) {
      const maxPositionLeverage = positionsPerp.maxPositionLeverage({
        IMRFactor,
        notional,
      });
      return Math.min(maxPositionLeverage, maxLeverage);
    }

    // when user has no existing position
    return maxLeverage;
  }, [position, maxLeverage, symbol]);

  /** calculate maximum position at current leverage */
  const maxPositionNotional = useMemo(() => {
    const IMRFactor = accountInfo?.imr_factor?.[symbol];
    if (leverage && IMRFactor) {
      return positionsPerp.maxPositionNotional({
        leverage,
        IMRFactor,
      });
    }
  }, [leverage, symbol]);

  const overMaxPositionLeverage = useMemo(() => {
    return leverage > maxPositionLeverage;
  }, [leverage, maxPositionLeverage]);

  // calc free collateral with new leverage
  const freeCollateral = useMemo(() => {
    if (!accountInfo || !markPrices || !symbolsInfo) {
      return zero;
    }

    const positionList = leverage
      ? positions?.rows.map((item) => {
          if (item.symbol === symbol && item.margin_mode === marginMode) {
            return {
              ...item,
              leverage,
            };
          }
          return item;
        })
      : positions?.rows;
    const maxLeverageBySymbol =
      positionList?.reduce<Record<string, number>>((acc, item) => {
        if (
          item.margin_mode !== MarginMode.ISOLATED &&
          item.leverage &&
          !acc[item.symbol]
        ) {
          acc[item.symbol] = item.leverage;
        }
        return acc;
      }, {}) ?? {};

    const totalInitialMarginWithOrders = accountPerp.totalInitialMarginWithQty({
      positions: positionList,
      markPrices,
      IMR_Factors: accountInfo.imr_factor,
      maxLeverageBySymbol,
      symbolInfo: symbolsInfo,
    });

    const freeCollateral = accountPerp.freeCollateral({
      totalCollateral,
      totalInitialMarginWithOrders,
    });

    return freeCollateral;
  }, [
    positions,
    symbolsInfo,
    accountInfo,
    markPrices,
    totalCollateral,
    leverage,
    symbol,
    marginMode,
  ]);

  const overRequiredMargin = useMemo(() => {
    return freeCollateral.eq(0) || freeCollateral.isNegative();
  }, [freeCollateral]);

  return {
    position,
    freeCollateral,
    maxPositionNotional,
    maxPositionLeverage,
    overMaxPositionLeverage,
    overRequiredMargin,
  };
}
