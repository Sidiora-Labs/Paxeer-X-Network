import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text } from "@paxeer-network/ui";
import { TradeDataState } from "./tradeData.script";

export const TradeData: FC<TradeDataState> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"column"} gap={2}>
      <Row
        title={t("common.markPrice")}
        value={props.ticker?.mark_price}
        dp={props.symbolInfo.quote_dp}
      />
      <Row
        title={t("common.indexPrice")}
        value={props.ticker?.index_price}
        dp={props.symbolInfo.quote_dp}
      />
      <Row
        title={t("markets.column.24hVolume")}
        value={props.vol_24h}
        dp={props.symbolInfo.quote_dp}
        rule="human"
        showUSDC
      />
      <Row
        title={t("trading.column.24High")}
        value={props.ticker?.["24h_high"]}
        dp={props.symbolInfo.quote_dp}
      />
      <Row
        title={t("trading.column.24Low")}
        value={props.ticker?.["24h_low"]}
        dp={props.symbolInfo.quote_dp}
      />
      <Row
        title={t("markets.openInterest")}
        value={props.openInterest}
        dp={props.symbolInfo.quote_dp}
        rule="human"
        showUSDC
      />
    </Flex>
  );
};

const Row = (props: {
  title: string;
  value: any;
  dp?: number;
  rule?: "percentages" | "price" | "human";
  showUSDC?: boolean;
}) => {
  const { title, value, showUSDC, dp, rule = "price" } = props;
  return (
    <Flex justify={"between"} width={"100%"} className="oui-text-xs">
      <Text intensity={36}>{title}</Text>
      <Flex gap={1}>
        <Text.numeral rule={rule} dp={dp} intensity={80}>
          {value}
        </Text.numeral>
        {showUSDC && <Text intensity={36}>USDC</Text>}
      </Flex>
    </Flex>
  );
};
