/* eslint-disable @typescript-eslint/no-explicit-any */
import React from "react";
import {
  AssetLineChart,
  PnlAreaChart,
  PnLBarChart,
  PnlLineChartProps,
  VolBarChart,
  VolumeAreaChart,
} from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import { Tabs, TabPanel, Flex, Text } from "@paxeer-network/ui";
import type { UsePerformanceScriptReturn } from "../performance/performance.script";
import {
  PeriodType,
  useAssetsHistoryDataReturn,
} from "../shared/useAssetHistory";

const responsiveProps: PnlLineChartProps["responsiveContainerProps"] = {
  width: "100%",
  minHeight: 144,
  aspect: 2.5,
};

export const PerformanceMobileUI: React.FC<
  Pick<
    useAssetsHistoryDataReturn & UsePerformanceScriptReturn,
    | "data"
    | "curPeriod"
    | "aggregateValue"
    | "onPeriodChange"
    | "invisible"
    | "visible"
    | "createFakeData"
    | "period"
  >
> = (props) => {
  const { t } = useTranslation();
  const {
    data,
    aggregateValue,
    visible,
    invisible,
    curPeriod,
    period,
    onPeriodChange,
    createFakeData,
  } = props;

  const mergedData = data.length
    ? data
    : (createFakeData?.(
        { account_value: 0, pnl: 0 },
        { account_value: 500, pnl: 500 },
      ) as any[]);

  const volumeData = React.useMemo(
    () =>
      (mergedData ?? []).map((d) => ({
        ...d,
        volume: d.perp_volume ?? 0,
      })),
    [mergedData],
  );

  return (
    <div>
      <Tabs
        value={period}
        defaultValue={PeriodType.WEEK}
        classNames={{ tabsList: "oui-justify-between", trigger: "oui-w-1/3" }}
        onValueChange={(value) => onPeriodChange(value as PeriodType)}
      >
        <TabPanel
          title={t("common.select.7d")}
          value={PeriodType.WEEK}
          className="oui-min-h-40"
        >
          <AssetLineChart
            data={mergedData}
            invisible={invisible || (data?.length ?? 0) < 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
        <TabPanel
          title={t("common.select.30d")}
          value={PeriodType.MONTH}
          className="oui-min-h-40"
        >
          <AssetLineChart
            data={mergedData}
            invisible={invisible || (data?.length ?? 0) < 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
        <TabPanel
          title={t("common.select.90d")}
          value={PeriodType.QUARTER}
          className="oui-min-h-40"
        >
          <AssetLineChart
            data={mergedData}
            invisible={invisible || (data?.length ?? 0) < 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
      </Tabs>
      <Flex justify="between" itemAlign="center" gap={2} my={4}>
        <Flex
          gap={1}
          direction="column"
          itemAlign="start"
          className="oui-w-1/3 oui-rounded-lg oui-bg-base-7 oui-px-2 oui-py-1.5"
        >
          <Text className="oui-truncate" intensity={36} size="2xs">
            {t("portfolio.overview.performance.roi", { period: curPeriod })}
          </Text>
          <Text.numeral size="sm" visible={visible} rule="percentages" coloring>
            {invisible ? "--" : aggregateValue.roi}
          </Text.numeral>
        </Flex>
        <Flex
          gap={1}
          direction="column"
          itemAlign="start"
          className="oui-w-1/3 oui-rounded-lg oui-bg-base-7 oui-px-2 oui-py-1.5"
        >
          <Text className="oui-truncate" intensity={36} size="2xs">
            {t("portfolio.overview.performance.pnl", { period: curPeriod })}
          </Text>
          <Text.numeral size="sm" visible={visible} coloring showIdentifier>
            {invisible ? "--" : aggregateValue.pnl}
          </Text.numeral>
        </Flex>
        <Flex
          gap={1}
          direction="column"
          itemAlign="start"
          className="oui-w-1/3 oui-rounded-lg oui-bg-base-7 oui-px-2 oui-py-1.5"
        >
          <Text className="oui-truncate" intensity={36} size="2xs">
            {t("portfolio.overview.performance.volume", { period: curPeriod })}
          </Text>
          <Text.numeral size="sm" visible={visible} coloring={false}>
            {invisible ? "--" : aggregateValue.vol}
          </Text.numeral>
        </Flex>
      </Flex>
      <Tabs defaultValue={"dailyPnl"} showScrollIndicator>
        <TabPanel
          value={"dailyPnl"}
          title={t("portfolio.overview.performance.dailyPnl")}
          className="oui-min-h-40"
        >
          <PnLBarChart
            data={mergedData}
            invisible={invisible || (mergedData?.length ?? 0) <= 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
        <TabPanel
          value={"cumulativePnl"}
          title={t("portfolio.overview.performance.cumulativePnl")}
          className="oui-min-h-40"
        >
          <PnlAreaChart
            data={mergedData}
            invisible={invisible || (mergedData?.length ?? 0) <= 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
        <TabPanel
          value={"dailyVolume"}
          title={t("portfolio.overview.performance.dailyVolume")}
          className="oui-min-h-40"
        >
          <VolBarChart
            data={volumeData}
            invisible={invisible || (mergedData?.length ?? 0) <= 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
        <TabPanel
          value={"cumulativeVolume"}
          title={t("portfolio.overview.performance.cumulativeVolume")}
          className="oui-min-h-40"
        >
          <VolumeAreaChart
            data={mergedData}
            invisible={invisible || (mergedData?.length ?? 0) <= 2}
            responsiveContainerProps={responsiveProps}
          />
        </TabPanel>
      </Tabs>
    </div>
  );
};
