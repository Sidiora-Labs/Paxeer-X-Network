import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Select, Text } from "@paxeer-network/ui";
import { SummaryReturns } from "./summary.script";

export const Summary: FC<SummaryReturns> = (props) => {
  const { statistics } = props;
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-affiliate-affiliate-summary"
      r={"2xl"}
      p={5}
      width={"100%"}
      height={"100%"}
      gap={6}
      direction={"column"}
      intensity={900}
      className="oui-affiliate-summary oui-border oui-border-line-6"
    >
      <Flex direction={"row"} justify={"between"} width={"100%"}>
        <Text className="oui-text-lg">{t("affiliate.summary")}</Text>
        <div className={"oui-min-w-14"}>
          <Select.options
            size={"xs"}
            value={props.period}
            onValueChange={props.onPeriodChange}
            options={props.periodTypes}
          />
        </div>
      </Flex>

      <SummaryItem
        prefix="$"
        rule="human"
        label={`${t("affiliate.commission")} (USDC)`}
        value={statistics.total_rebate}
        direct={statistics.direct_rebate + statistics.direct_bonus_rebate}
        indirect={statistics.indirect_rebate}
        referredByCode={props.referredByCode}
        classNames={{
          root: "!oui-py-12",
          value: "oui-text-trade-profit",
          direct: "oui-text-trade-profit",
          indirect: "oui-text-trade-profit",
        }}
      />

      <Flex
        width={"100%"}
        gap={6}
        itemAlign="stretch"
        className="oui-flex-col md:oui-flex-row"
      >
        <SummaryItem
          prefix="$"
          rule="human"
          label={`${t("affiliate.referralVol")} (USDC)`}
          value={statistics.total_volume}
          direct={statistics.direct_volume}
          indirect={statistics.indirect_volume}
          classNames={{
            root: "oui-w-full md:oui-w-1/2 md:oui-h-full",
          }}
        />
        <SummaryItem
          dp={0}
          label={t("affiliate.referrals")}
          value={statistics.total_invites}
          direct={statistics.direct_invites}
          indirect={statistics.indirect_invites}
          classNames={{
            root: "oui-w-full md:oui-w-1/2 md:oui-h-full",
          }}
        />
      </Flex>
    </Flex>
  );
};

type SummaryItemProps = {
  label: string;
  value: number | string;
  direct: number | string;
  indirect: number | string;
  classNames?: {
    root?: string;
    value?: string;
    direct?: string;
    indirect?: string;
  };
  prefix?: string;
  rule?: "human";
  dp?: number;
  referredByCode?: string;
};

const SummaryItem: FC<SummaryItemProps> = (props) => {
  const { t } = useTranslation();
  const { rule, dp = 2 } = props;

  return (
    <Flex
      r="2xl"
      width={"100%"}
      direction={"column"}
      itemAlign="start"
      gap={2}
      className={cn(
        "oui-bg-base-contrast-4 oui-p-5 md:oui-py-[38px]",
        "oui-text-sm oui-text-base-contrast-54",
        props.classNames?.root,
      )}
    >
      <Text>{props.label}</Text>
      <Text.numeral
        rule={rule}
        dp={dp}
        size="3xl"
        prefix={props.prefix}
        placeholder="--"
        intensity={98}
        className={props.classNames?.value}
      >
        {props.value}
      </Text.numeral>

      <Flex className="oui-flex-wrap oui-text-base-contrast-54">
        <Flex gap={1}>
          <Text>{t("affiliate.direct")}:</Text>
          <Text.numeral
            rule={rule}
            dp={dp}
            prefix={props.prefix}
            intensity={54}
            className={props.classNames?.direct}
          >
            {props.direct}
          </Text.numeral>
        </Flex>
        {"/"}
        <Flex gap={1}>
          <Text>{t("affiliate.indirect")}:</Text>
          <Text.numeral
            rule={rule}
            dp={dp}
            prefix={props.prefix}
            intensity={54}
            className={props.classNames?.indirect}
          >
            {props.indirect}
          </Text.numeral>
        </Flex>
      </Flex>

      {props.referredByCode ? (
        <Text
          intensity={54}
          className="oui-affiliate-summary-referred-by oui-mt-5 oui-text-base-contrast-54"
        >
          {t("affiliate.referredBy", { name: props.referredByCode })}
        </Text>
      ) : null}
    </Flex>
  );
};
