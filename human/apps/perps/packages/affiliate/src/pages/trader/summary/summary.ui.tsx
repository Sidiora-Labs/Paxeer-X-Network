import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, Flex, Select, Text } from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";
import { USDCIcon } from "../../../icons/usdcIcon";
import { SummaryReturns } from "./summary.script";

export const Summary: FC<SummaryReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-affiliate-trader-summary"
      r={"2xl"}
      p={6}
      width={"100%"}
      height={"100%"}
      gap={4}
      direction={"column"}
      className="oui-bg-base-9 xl:oui-flex-1"
    >
      <Title {...props} />
      <Rebate className="md:oui-hidden" {...props} />
      <Flex direction={"column"} gap={3} width={"100%"} className="md:oui-mt-3">
        <SummaryFilter {...props} />
        <CommissionData {...props} />
      </Flex>
      <Row title={`${t("affiliate.trader.tradingVol")} (USDC)`} {...props} />
    </Flex>
  );
};

const Title: FC<SummaryReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"row"} width={"100%"} gap={3}>
      <Flex direction={"row"} gap={3} className="oui-flex-1">
        <Text className="oui-text-lg">
          {t("affiliate.trader.yourReferrer")}
        </Text>
        <Button
          color="secondary"
          size="sm"
          className="oui-text-primary-light oui-text-2xs md:oui-text-xs 2xl:oui-text-sm oui-bg-base-6"
        >
          {props.code}
        </Button>
      </Flex>
      <Rebate
        className="oui-hidden md:oui-flex md:oui-flex-shrink md:oui-w-fit"
        {...props}
      />
    </Flex>
  );
};

const Rebate: FC<SummaryReturns & { className?: string }> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"row"} width={"100%"} gap={2} className={props.className}>
      <Text
        intensity={54}
        className="oui-text-sm md:oui-text-base 2xl:oui-text-lg"
      >
        {`${t("affiliate.trader.rebate")}:`}
      </Text>
      <Text.gradient
        color="brand"
        className="oui-text-lg md:oui-text-xl 2xl:oui-text-2xl"
      >
        {props.rebateText}
      </Text.gradient>
    </Flex>
  );
};

const SummaryFilter: FC<SummaryReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex direction={"row"} width={"100%"} justify={"between"} gap={3}>
      <Text className="oui-text-sm">{t("affiliate.summary")}</Text>
      <div>
        <Select.options
          size={"xs"}
          value={props.period}
          onValueChange={props.onPeriodChange}
          options={props.periodTypes}
        />
      </div>
    </Flex>
  );
};

const CommissionData: FC<SummaryReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      gradient="success"
      angle={180}
      r="xl"
      py={4}
      px={6}
      width={"100%"}
      direction={"column"}
      gap={3}
    >
      <Text intensity={54} className="oui-text-base 2xl:oui-text-lg">
        {`${t("affiliate.trader.rebates")} (USDC)`}
      </Text>
      <Flex
        direction={"row"}
        gap={3}
        className="oui-text-xl md:oui-text-2xl xl:oui-text-3xl"
      >
        <USDCIcon className="md:oui-w-[24px] md:oui-h-[24px] lg:oui-w-[28px] lg:oui-h-[28px] " />
        <Text>{commifyOptional(props.rebates, { fix: 2, fallback: "0" })}</Text>
      </Flex>
    </Flex>
  );
};

const Row: FC<
  SummaryReturns & {
    title: string;
  }
> = (props) => {
  return (
    <Flex direction={"row"} justify={"between"} width={"100%"}>
      <Text
        intensity={54}
        className="oui-text-2xs md:oui-text-xs xl:oui-text-sm"
      >
        {props.title}
      </Text>
      <Text className="oui-text-xs md:oui-text-sm xl:oui-text-base">
        {commifyOptional(props.vol, { fix: 2, fallback: "0" })}
      </Text>
    </Flex>
  );
};
