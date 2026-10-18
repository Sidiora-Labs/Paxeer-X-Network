import { useState } from "react";
import {
  useLeverage,
  useLocalStorage,
  useMarginRatio,
  usePositionStream,
} from "@paxeer-network/hooks";
import { useCollateral, useAccount } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";

export type AccountSummaryType =
  | "totalValue"
  | "freeCollateral"
  | "unrealPnL"
  | "currentLeverage"
  | "maxLeverage";

export const useTotalValueBuilderScript = () => {
  const [keys, setKeys] = useLocalStorage<string[]>("accountSummaryTypes", [
    "totalValue",
  ]);

  const [elementKeys, setElementKeys] = useState<string[]>([
    "totalValue",
    "freeCollateral",
    "unrealPnL",
    "currentLeverage",
    "maxLeverage",
  ]);

  const { freeCollateral, totalValue } = useCollateral({ dp: 2 });

  const { state } = useAccount();

  const [visible, setVisible] = useLocalStorage("paxeerNetwork_assets_visible", true);

  const [{ aggregated, totalUnrealizedROI }] = usePositionStream();
  const { wrongNetwork, disabledConnect } = useAppContext();

  const { currentLeverage } = useMarginRatio();

  const { maxLeverage } = useLeverage();

  const onToggleItemByKey = (key: string) => {
    if (keys.includes(key)) {
      setKeys(keys.filter((k: string) => k !== key));
    } else {
      setKeys([...keys, key]);
    }
  };

  const onKeyToTop = (key: string) => {
    if (!keys.includes(key)) {
      setKeys([key, ...keys]);
      setElementKeys([key, ...elementKeys.filter((k: string) => k !== key)]);
      return;
    }

    setKeys([key, ...keys.filter((k: string) => k !== key)]);
    setElementKeys([key, ...elementKeys.filter((k: string) => k !== key)]);
  };

  const unavailable =
    wrongNetwork ||
    disabledConnect ||
    (state.status < AccountStatusEnum.EnableTrading &&
      state.status !== AccountStatusEnum.EnableTradingWithoutConnected);

  return {
    totalValue: unavailable ? null : totalValue,
    freeCollateral: unavailable ? null : freeCollateral,
    maxLeverage: unavailable ? null : maxLeverage,
    currentLeverage: unavailable ? null : currentLeverage,
    unrealPnL: unavailable ? null : aggregated?.total_unreal_pnl,
    unrealized_pnl_ROI: unavailable ? null : totalUnrealizedROI,
    // type,
    keys,
    elementKeys,
    // onTypeChange,
    visible,
    wrongNetwork,
    onToggleItemByKey,
    onKeyToTop,
    onToggleVisibility: () => setVisible(!visible),
  };
};

export type UseTotalValueBuilderScript = ReturnType<
  typeof useTotalValueBuilderScript
>;
