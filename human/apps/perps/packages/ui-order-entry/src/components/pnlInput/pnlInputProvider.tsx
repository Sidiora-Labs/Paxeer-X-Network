import React, { useMemo } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { PnlInputContext, PnlInputContextState } from "./pnlInputContext";
import { PNL_Values, PnLMode } from "./useBuilder.script";

export const PnlInputProvider: React.FC<
  React.PropsWithChildren<{
    values: PNL_Values & { trigger_price?: string };
    type: "TP" | "SL";
  }>
> = (props) => {
  const { type, values, children } = props;
  const [mode, setMode] = useLocalStorage<PnLMode>(
    "TP/SL_Mode",
    PnLMode.PERCENTAGE,
  );
  const { t } = useTranslation();

  const tipsEle = useMemo(() => {
    if (!values.PnL || !values.trigger_price) {
      return null;
    }
    return (
      <Flex>
        <span className={"oui-text-xs oui-text-base-contrast-54"}>
          {mode === PnLMode.PnL
            ? t("orderEntry.estRoi")
            : t("orderEntry.estPnL")}
        </span>
        {mode === PnLMode.PnL ? (
          <Text.numeral
            rule={"percentages"}
            className={cn(
              "oui-ms-1 oui-text-xs",
              type === "TP" ? "oui-text-trade-profit" : "oui-text-trade-loss",
            )}
          >
            {values.ROI}
          </Text.numeral>
        ) : (
          <Text.numeral
            rule={"price"}
            className={cn(
              "oui-ms-1 oui-text-xs",
              type === "TP" ? "oui-text-trade-profit" : "oui-text-trade-loss",
            )}
          >
            {values.PnL}
          </Text.numeral>
        )}
      </Flex>
    );
  }, [mode, t, type, values.ROI, values.PnL, values.trigger_price]);

  const memoizedValue = useMemo<PnlInputContextState>(() => {
    return { mode, setMode, tipsEle };
  }, [mode, setMode, tipsEle]);

  return (
    <PnlInputContext.Provider value={memoizedValue}>
      {children}
    </PnlInputContext.Provider>
  );
};
