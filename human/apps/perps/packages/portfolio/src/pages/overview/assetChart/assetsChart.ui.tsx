/* eslint-disable @typescript-eslint/no-explicit-any */
import React from "react";
import { AssetAreaChart } from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import { Card } from "@paxeer-network/ui";
import { useAssetsChartScriptReturn } from "./assetsChart.script";

const LazyPeriodTitle = React.lazy(() =>
  import("../shared/periodHeader").then((mod) => {
    return { default: mod.PeriodTitle };
  }),
);

export type AssetsLineChartProps = {} & useAssetsChartScriptReturn;

export const AssetsChart: React.FC<AssetsLineChartProps> = (props) => {
  const { onPeriodChange, data, periodTypes, period } = props;
  const { t } = useTranslation();
  return (
    <Card
      title={
        <React.Suspense fallback={null}>
          <LazyPeriodTitle
            onPeriodChange={onPeriodChange}
            periodTypes={periodTypes}
            period={period}
            title={t("common.assets")}
          />
        </React.Suspense>
      }
      id="portfolio-overview-assets-chart"
      classNames={{ content: "oui-h-[168px] oui-pb-0" }}
    >
      <React.Suspense fallback={null}>
        <AssetAreaChart data={data as any} invisible={props.invisible} />
      </React.Suspense>
    </Card>
  );
};
