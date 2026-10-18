/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useId } from "react";
import { Area, AreaChart, ResponsiveContainer } from "@paxeer-network/chart";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import { ChevronRightIcon, cn, Flex, Text } from "@paxeer-network/ui";
import { usePortfolioChartsState } from ".";

export const PortfolioChartsMobileUI: React.FC<
  ReturnType<typeof usePortfolioChartsState> & {
    data: any[];
    invisible?: boolean;
  }
> = (props) => {
  const {
    data,
    invisible,
    todaysPnl,
    todaysRoi,
    todaysVol,
    visible,
    onPerformanceClick,
  } = props;
  const { t } = useTranslation();
  const colorId = useId();
  return (
    <Flex
      p={4}
      width={"100%"}
      direction="column"
      className={cn(
        "oui-relative oui-overflow-hidden oui-rounded-2xl oui-border oui-border-solid oui-border-line-12 oui-bg-base-9",
      )}
    >
      <Flex width={"100%"} justify={"between"} itemAlign={"start"} mb={3}>
        <Flex direction="column" gap={1}>
          <Text size="sm" intensity={54}>
            {t("portfolio.overview.todaysPnl")}
          </Text>
          <Flex justify={"start"} itemAlign={"center"} gapX={1}>
            <Text.pnl coloring size="base" weight="semibold" visible={visible}>
              {invisible || todaysPnl == null ? "--" : todaysPnl}
            </Text.pnl>
            <Text.roi
              coloring
              rule="percentages"
              size="sm"
              weight="semibold"
              prefix={"("}
              suffix={")"}
              visible={visible}
            >
              {invisible || todaysRoi == null ? "--" : todaysRoi}
            </Text.roi>
          </Flex>
        </Flex>
        <Flex direction="column" gap={1} itemAlign="end">
          <Text size="sm" intensity={54}>
            {t("portfolio.overview.todaysVol")}
          </Text>
          <Text.numeral
            size="sm"
            rule="human"
            dp={2}
            visible={visible}
            prefix="$"
          >
            {invisible || todaysVol == null ? "--" : todaysVol}
          </Text.numeral>
        </Flex>
      </Flex>

      <Flex
        width={"100%"}
        justify={"center"}
        itemAlign={"center"}
        mb={3}
        className="oui-px-1"
      >
        <ResponsiveContainer width="100%" height={52}>
          <AreaChart data={data || EMPTY_LIST}>
            {!invisible && (
              <>
                <defs>
                  <linearGradient id={colorId} x1="0" y1="0" x2="0" y2="1">
                    <stop
                      stopColor="rgb(var(--oui-color-success))"
                      offset="0%"
                      stopOpacity={0.5}
                    />
                    <stop
                      stopColor="rgb(var(--oui-color-success))"
                      offset="100%"
                      stopOpacity={0}
                    />
                  </linearGradient>
                </defs>
                <Area
                  type="natural"
                  dataKey="account_value"
                  stroke="rgb(var(--oui-color-trading-profit))"
                  strokeWidth={1.5}
                  dot={false}
                  isAnimationActive={false}
                  fill={`url(#${colorId})`}
                />
              </>
            )}
          </AreaChart>
        </ResponsiveContainer>
      </Flex>

      <Flex width={"100%"} justify={"start"} itemAlign={"center"} mt={1}>
        <Text
          size="xs"
          intensity={54}
          className="oui-flex oui-items-center oui-justify-start oui-gap-1"
          onClick={onPerformanceClick}
        >
          {t("portfolio.overview.performance")}
          <ChevronRightIcon className="oui-text-base-contrast-54" />
        </Text>
      </Flex>
    </Flex>
  );
};
