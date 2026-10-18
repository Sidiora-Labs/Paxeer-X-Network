import React, { FC, useState, useEffect } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Flex,
  Text,
  TradingRewardsIcon,
  ChevronRightIcon,
  PaxeerNetworkIcon,
  EsPaxeerNetworkIcon,
  cn,
  modal,
} from "@paxeer-network/ui";
import type { UseRewardsDataReturn } from "./useRewardsData.script";

const isNumber = (val: unknown): val is number => {
  return typeof val === "number" && !Number.isNaN(val);
};

type TradingRewardsCardMobileProps = UseRewardsDataReturn & {
  isSignIn: boolean;
  wrongNetwork: boolean;
  goToClaim: () => void;
};

export const TradingRewardsCardMobile: FC<TradingRewardsCardMobileProps> = (
  props,
) => {
  const { t } = useTranslation();

  const curEpochInfo = props.epochList?.[1].curEpochInfo;
  // const startTime = curEpochInfo?.start_time;
  const endTime = curEpochInfo?.end_time;
  const curEpochId = curEpochInfo?.epoch_id;
  // const max_reward_amount = curEpochInfo?.max_reward_amount;

  const isOrder = curEpochInfo
    ? `${curEpochInfo?.epoch_token}`.toLowerCase() === "order"
    : undefined;

  const onClaim = (event: React.MouseEvent<SVGSVGElement>) => {
    if (!props.isSignIn || props.wrongNetwork) {
      event.stopPropagation();
      event.preventDefault();
      modal.alert({
        title: t("common.tips"),
        message: (
          <Text intensity={54}>
            {props.wrongNetwork
              ? t("connector.wrongNetwork.tooltip")
              : t("affiliate.connectWallet.tooltip")}
          </Text>
        ),
      });
    } else {
      props.goToClaim();
    }
  };

  return (
    <Flex
      className="
        oui-portfolio-trading-rewards-card
        oui-via-21.6% 
        oui-via-83.23% 
        oui-relative 
        oui-h-[236px] 
        oui-w-full 
        oui-flex-col 
        oui-items-start
        oui-rounded-xl 
        oui-border 
        oui-border-solid 
        oui-border-[rgba(var(--oui-gradient-secondary-start)/0.36)] 
        oui-bg-gradient-to-r
        oui-from-[rgba(var(--oui-gradient-secondary-end)/0.12)]
        oui-to-[rgba(var(--oui-gradient-secondary-start)/0.12)]
        oui-p-3
      "
    >
      <Flex className="oui-w-full oui-flex-row oui-justify-between oui-items-center">
        <Flex className="oui-flex-col oui-items-start">
          <Text className="oui-text-base-contrast oui-text-base oui-font-semibold oui-whitespace-pre max-[400px]:oui-text-[15px]">
            {t("common.tradingRewards")}
          </Text>
          <Text className="oui-text-2xs oui-font-normal oui-whitespace-pre oui-text-base-contrast-54">
            {t("tradingRewards.subtitle.mm")}
          </Text>
        </Flex>
        <TradingRewardsIcon />
      </Flex>
      <Flex
        direction="row"
        itemAlign={"center"}
        className="oui-mb-3 oui-mt-[35px] oui-w-full oui-justify-center oui-gap-1.5"
      >
        <Text className="oui-text-sm oui-font-normal oui-text-base-contrast-80">
          {t("tradingRewards.epoch")}
        </Text>
        <Text className="oui-text-base-contrast oui-text-sm oui-font-bold">
          {curEpochId}
        </Text>
      </Flex>
      <Countdown targetTimestamp={endTime} />
      <Flex className="oui-mt-auto oui-w-full oui-flex-row oui-items-center oui-gap-1">
        {isOrder ? (
          <PaxeerNetworkIcon className="oui-size-5" />
        ) : (
          <EsPaxeerNetworkIcon className="oui-size-5" />
        )}
        <Text
          className={cn(
            "oui-text-xs oui-font-semibold",
            isNumber(props?.curEpochEstimate?.est_r_wallet)
              ? "oui-text-base-contrast"
              : "oui-text-base-contrast-36",
          )}
        >
          {props?.curEpochEstimate?.est_r_wallet ?? "--"}
        </Text>
        <Flex className="oui-ms-auto">
          <ChevronRightIcon
            size={18}
            color="white"
            className="oui-ms-auto"
            onClick={onClaim}
          />
        </Flex>
      </Flex>
    </Flex>
  );
};

const Countdown: FC<{ targetTimestamp?: number }> = ({ targetTimestamp }) => {
  const [timeLeft, setTimeLeft] = useState({
    days: 0,
    hours: 0,
    minutes: 0,
    seconds: 0,
  });

  useEffect(() => {
    const intervalId = setInterval(() => {
      if (typeof targetTimestamp === "undefined") {
        return;
      }
      const now = new Date().getTime();
      const distance = targetTimestamp - now;

      if (distance < 0) {
        clearInterval(intervalId as unknown as number);
        setTimeLeft({
          days: 0,
          hours: 0,
          minutes: 0,
          seconds: 0,
        });
      } else {
        const days = Math.floor(distance / (1000 * 60 * 60 * 24));
        const hours = Math.floor(
          (distance % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60),
        );
        const minutes = Math.floor((distance % (1000 * 60 * 60)) / (1000 * 60));
        const seconds = Math.floor((distance % (1000 * 60)) / 1000);

        setTimeLeft({ days, hours, minutes, seconds });
      }
    }, 1000);

    return () => clearInterval(intervalId as unknown as number);
  }, [targetTimestamp]);

  return (
    <Flex
      direction="row"
      itemAlign={"center"}
      className="oui-w-full oui-justify-around"
    >
      <CountDownItem type="D" value={timeLeft.days} />
      <CountDownItem type="H" value={timeLeft.hours} />
      <CountDownItem type="M" value={timeLeft.minutes} />
      <CountDownItem type="S" value={timeLeft.seconds} />
    </Flex>
  );
};

const CountDownItem: React.FC<{ type: string; value: number }> = (props) => {
  const { type, value } = props;
  return (
    <Flex
      direction="column"
      itemAlign={"center"}
      className="oui-h-11 oui-w-8 oui-rounded-[6px] oui-bg-base-contrast-8"
    >
      <Text className="oui-text-base oui-font-bold oui-text-base-contrast">
        {value}
      </Text>
      <Text className="oui-text-2xs oui-font-normal oui-text-base-contrast-36">
        {type}
      </Text>
    </Flex>
  );
};
