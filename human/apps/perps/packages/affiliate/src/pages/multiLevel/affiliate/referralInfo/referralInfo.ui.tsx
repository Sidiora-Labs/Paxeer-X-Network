import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Flex,
  Text,
  Button,
  Divider,
  Tooltip,
  cn,
  toast,
} from "@paxeer-network/ui";
import { ReferralCodeFormField } from "../../../../types";
import { ReferralInfoReturns } from "./referralInfo.script";

export const ReferralInfo: FC<ReferralInfoReturns> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      id="oui-affiliate-affiliate-referralLink"
      r={"2xl"}
      width={"100%"}
      height={"100%"}
      gap={4}
      p={5}
      direction={"column"}
      itemAlign="start"
      intensity={900}
      className="oui-affiliate-referralInfo oui-border oui-border-line-6"
    >
      <Flex
        direction={"row"}
        justify={"between"}
        width={"100%"}
        className="oui-referralInfo-header"
      >
        <Text size="lg">{t("affiliate.referral")}</Text>
        {props.showConfigureButton ? (
          <Button
            size="xs"
            color="secondary"
            onClick={() => props.onEdit(ReferralCodeFormField.ReferralCode)}
          >
            {t("affiliate.configure")}
          </Button>
        ) : null}
      </Flex>

      <Container className="oui-referralInfo-code oui-flex-col oui-items-start oui-gap-4 oui-p-5">
        <Flex
          r="full"
          px={2}
          height={20}
          justify={"center"}
          itemAlign={"center"}
          className="oui-border oui-border-primary-light"
        >
          <Text intensity={98} className="oui-text-[10px] oui-leading-[10px]">
            {t("affiliate.multiLevel")}
          </Text>
        </Flex>
        <Flex direction={"row"} justify={"between"} width={"100%"}>
          <Text size="lg" color="primaryLight">
            {props.referralCode}
          </Text>
          <CopyButton value={props.referralCode} />
        </Flex>
      </Container>

      <Container className="oui-referralInfo-link oui-p-5">
        <Text size="lg" color="primaryLight">
          {props.referralLink}
        </Text>
        <CopyButton value={props.referralLink} />
      </Container>

      <Text
        size="lg"
        intensity={98}
        className="oui-w-full oui-text-left oui-tracking-[0.03em]"
      >
        {t("affiliate.yourCommissionRates")}
      </Text>

      <Flex
        width={"100%"}
        gap={4}
        itemAlign="stretch"
        direction={"row"}
        className="oui-affiliate-referralInfo-commissionRates"
      >
        <CommissionRateCard
          title={t("affiliate.directTrades")}
          rate={props.directTradesRate}
          description={t("affiliate.directTradesDescription")}
          className="oui-affiliate-referralInfo-directTrades"
        />
        {props.showIndirectTrades ? (
          <CommissionRateCard
            title={t("affiliate.indirectTrades")}
            rate={props.indirectTradesRate}
            description={t("affiliate.indirectTradesDescription")}
            className="oui-affiliate-referralInfo-indirectTrades"
          />
        ) : null}
      </Flex>

      {props.showTradingFeeRebate && (
        <>
          <Divider intensity={6} className="oui-w-full" />
          <Flex
            width="100%"
            justify="between"
            itemAlign="center"
            gap={2}
            p={5}
            r="2xl"
            className="oui-affiliate-referralInfo-tradingFeeRebate oui-bg-base-contrast-4"
          >
            <Tooltip
              content={t("affiliate.tradingFeeRebate.description")}
              className="oui-max-w-[280px]"
            >
              <Text
                size="base"
                intensity={98}
                tabIndex={0}
                className="oui-min-w-0 oui-cursor-help oui-leading-6 oui-tracking-[0.03em] oui-underline oui-decoration-dotted oui-underline-offset-[6px]"
              >
                {t("affiliate.tradingFeeRebate")}: {props.tradingFeeRebateRate}%
              </Text>
            </Tooltip>
            <Button
              size="xs"
              color="secondary"
              className="oui-shrink-0"
              onClick={props.onSetTradingFeeRebate}
            >
              {t("affiliate.tradingFeeRebate.set")}
            </Button>
          </Flex>
        </>
      )}
    </Flex>
  );
};

type CommissionRateCardProps = {
  title: string;
  rate: number;
  description: string;
  className?: string;
};

const CommissionRateCard: FC<CommissionRateCardProps> = (props) => {
  return (
    <Flex
      r="2xl"
      direction={"column"}
      itemAlign="start"
      justify="center"
      p={5}
      className={cn(
        "oui-min-w-0 oui-flex-1 oui-bg-base-contrast-4",
        props.className,
      )}
    >
      <Flex direction={"column"} gap={4} width={"100%"} itemAlign="start">
        <Text
          size="sm"
          intensity={98}
          weight="semibold"
          className="oui-w-full oui-tracking-[0.03em]"
        >
          {props.title}
        </Text>

        <Flex direction={"column"} gap={2} width={"100%"} itemAlign="start">
          <Text
            size="3xl"
            weight="semibold"
            color="success"
            className="oui-leading-9 oui-tracking-[0.03em]"
          >
            {props.rate}%
          </Text>
          <Text
            size="sm"
            intensity={54}
            weight="semibold"
            className="oui-w-full oui-leading-5 oui-tracking-[0.03em]"
          >
            {props.description}
          </Text>
        </Flex>
      </Flex>
    </Flex>
  );
};

const Container: FC<{
  children: React.ReactNode;
  className?: string;
}> = (props) => {
  return (
    <Flex
      width={"100%"}
      justify={"between"}
      px={5}
      r="2xl"
      className={cn("oui-bg-base-contrast-4", props.className)}
    >
      {props.children}
    </Flex>
  );
};

const CopyButton: FC<{
  value?: string;
}> = (props) => {
  const { t } = useTranslation();

  const onCopy = () => {
    if (!props.value) return;
    navigator.clipboard.writeText(props.value);
    toast.success(t("common.copy.copied"));
  };

  return (
    <Button size="xs" color="secondary" onClick={onCopy}>
      {t("common.copy")}
    </Button>
  );
};
