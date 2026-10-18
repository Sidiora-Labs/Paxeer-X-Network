import React from "react";
import { pick } from "ramda";
import { i18n } from "@paxeer-network/i18n";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { usePerformanceScript } from "..";
import { localKey } from "../provider/overviewProvider";
import { useAssetsHistoryData } from "../shared/useAssetHistory";
import { PerformanceMobileUI } from "./performance.ui";

export const PerformanceMobileWidget: React.FC = () => {
  const state = useAssetsHistoryData(localKey, { isRealtime: true });
  const { visible, invisible } = usePerformanceScript();
  return (
    <PerformanceMobileUI
      {...pick(
        [
          "data",
          "curPeriod",
          "aggregateValue",
          "onPeriodChange",
          "createFakeData",
          "period",
        ],
        state,
      )}
      visible={visible}
      invisible={invisible}
    />
  );
};

export const PerformanceMobileSheetId = "PerformanceMobileSheetId";

export const PerformanceMobileDialogId = "PerformanceMobileDialogId";

registerSimpleSheet(PerformanceMobileSheetId, PerformanceMobileWidget, {
  title: () => i18n.t("portfolio.overview.performance"),
});

registerSimpleDialog(PerformanceMobileDialogId, PerformanceMobileWidget, {
  title: () => i18n.t("portfolio.overview.performance"),
  classNames: {
    content: "oui-w-[420px]",
  },
});
