import { useState, useEffect, FC, ReactNode, SVGProps, useMemo } from "react";
import { EpochStatus } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Box, Flex, Text, cn } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { commifyOptional } from "@paxeer-network/utils";
import { EsPaxeerNetworkIcon } from "../components/esPaxeerNetworkIcon";
import { PaxeerNetworkIcon } from "../components/paxeerNetworkIcon";
import { CurEpochReturns } from "./curEpoch.script";
import { RewardsTooltip } from "./rewardsTooltip";

export const CurEpoch: FC<CurEpochReturns> = (props) => {
  const { t } = useTranslation();

  const state = props;
  const curEpochInfo = state.epochList?.[1].curEpochInfo;
  const epochList = state.epochList?.[0];
  const startTime = curEpochInfo?.start_time;
  const endTime = curEpochInfo?.end_time;
  const curEpochId = curEpochInfo?.epoch_id;
  const max_reward_amount = curEpochInfo?.max_reward_amount;
  const token = curEpochInfo?.epoch_token;

  const isOrder = curEpochInfo
    ? `${curEpochInfo?.epoch_token}`.toLowerCase() === "order"
    : undefined;

  const pausedEpochTimeDown = useMemo(() => {
    if (!props.showEpochPauseCountdown) {
      return undefined;
    }
    if (!epochList || props.statusInfo?.epochStatus !== EpochStatus.paused) {
      return undefined;
    }

    const lastCompletedEpoch = props.statusInfo?.lastCompletedEpoch;
    if (lastCompletedEpoch === undefined) {
      return undefined;
    }
    const nextEpoch = lastCompletedEpoch + 1;
    const epoch = epochList.find((item: any) => item.epoch_id === nextEpoch);
    return epoch?.start_time;
  }, [props.statusInfo?.epochStatus, epochList]);

  const showPauseCountdown = useMemo(() => {
    return (
      props.statusInfo?.epochStatus === EpochStatus.paused &&
      pausedEpochTimeDown
    );
  }, [props.statusInfo?.epochStatus, pausedEpochTimeDown]);

  if (props.statusInfo?.epochStatus !== EpochStatus.active) {
    return (
      <Flex
        id="oui-tradingRewards-home-currentEpoch"
        r={"2xl"}
        className="oui-bg-base-9 oui-font-semibold oui-p-10"
        width={"100%"}
        height={"100%"}
        direction={"column"}
        //   justify={"stretch"}
        itemAlign={"stretch"}
      >
        <Flex
          gap={2}
          direction={"column"}
          justify={"center"}
          itemAlign={"center"}
          className="oui-size-full"
        >
          {showPauseCountdown ? (
            <Flex
              gap={2}
              direction={"column"}
              justify={"center"}
              itemAlign={"center"}
            >
              <Text className="oui-text-base-contrast-54 oui-text-sm">
                {t("tradingRewards.epochPauseCountdown.title")}
              </Text>
              <Countdown targetTimestamp={pausedEpochTimeDown} isStandalone />
            </Flex>
          ) : (
            <Flex
              className="oui-text-base-contrast-54 oui-text-sm oui-text-center"
              justify={"center"}
              itemAlign={"center"}
            >
              {props.statusInfo?.epochStatus === EpochStatus.paused
                ? t("tradingRewards.eopchStatus.pause")
                : t("tradingRewards.eopchStatus.ended")}
            </Flex>
          )}
          {props.statusInfo?.epochStatus === EpochStatus.paused && (
            <div
              className={cn(
                "oui-w-full",
                showPauseCountdown ? "oui-mt-2" : "oui-mt-0",
              )}
            >
              <TwitterLInk />
            </div>
          )}
        </Flex>
      </Flex>
    );
  }

  return (
    <Flex
      id="oui-tradingRewards-home-currentEpoch"
      r={"2xl"}
      className="oui-bg-base-9 oui-font-semibold"
      width={"100%"}
      height={"100%"}
      direction={"column"}
      //   justify={"stretch"}
      itemAlign={"stretch"}
    >
      <Countdown targetTimestamp={endTime} />
      <Flex p={6} direction={"column"} gap={4} className="oui-h-full">
        <Flex direction={"row"} gap={3} width={"100%"} justify={"around"}>
          <Statics
            title={t("tradingRewards.epoch")}
            highLight={curEpochId ? `${curEpochId}` : "--"}
            text={
              startTime && endTime
                ? `${getDate(startTime)} - ${getDate(endTime)}`
                : ""
            }
          />
          <Statics
            title={t("tradingRewards.epochRewards")}
            highLight={commifyOptional(max_reward_amount, { fix: 0 })}
            text={token}
          />
        </Flex>
        <EstRewards
          isOrder={isOrder}
          direction={state.notConnected ? "row" : "column"}
          justify={state.notConnected ? "between" : "center"}
          hideData={state.hideData}
          estRewards={state.hideData ? "--" : props.estimate?.est_r_wallet}
          rewardsTooltip={props.rewardsTooltip}
          background={
            state.notConnected
              ? "linear-gradient(28.29deg, rgb(var(--oui-gradient-neutral-start)) 21.6%, rgb(var(--oui-gradient-neutral-end)) 83.23%)"
              : "linear-gradient(0deg, rgb(var(--oui-gradient-secondary-end)) 2.62%, rgb(var(--oui-gradient-secondary-start)) 86.5%)"
          }
        />
        <div className="oui-w-full">
          <AuthGuard
            status={AccountStatusEnum.SignedIn}
            buttonProps={{ fullWidth: true }}
          >
            <></>
          </AuthGuard>
        </div>
      </Flex>
    </Flex>
  );
};

const EstRewards: FC<{
  isOrder?: boolean;
  estRewards?: number | string;
  direction: "row" | "column";
  justify: "center" | "between";
  background: string;
  hideData: boolean;
  rewardsTooltip:
    | {
        brokerName: string | undefined;
        curRewards: number;
        otherRewards: number;
      }
    | undefined;
}> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      direction={props.direction}
      gap={2}
      py={4}
      px={6}
      width={"100%"}
      r="xl"
      itemAlign={"center"}
      justify={props.justify}
      style={{
        background: props.background,
      }}
      className="oui-flex-1 oui-h-full"
    >
      <Text className="oui-text-base xl:oui-text-lg oui-text-base-contrast-54">
        {t("tradingRewards.myEstRewards")}
      </Text>
      <Flex direction={"row"} gap={3}>
        {props.isOrder == true && (
          <PaxeerNetworkIcon className="oui-w-5 oui-h-5 md:oui-w-6 md:oui-h-6 lg:oui-w-7 lg:oui-h-7 xl:oui-w-8 xl:oui-h-8" />
        )}
        {props.isOrder == false && (
          <EsPaxeerNetworkIcon className="oui-w-5 oui-h-5 md:oui-w-6 md:oui-h-6 lg:oui-w-7 lg:oui-h-7 xl:oui-w-8 xl:oui-h-8" />
        )}
        <Text
          children={commifyOptional(props.estRewards, { fix: 2 })}
          className="oui-text-xl md:oui-text-2xl xl:oui-text-[32px]"
        />
        {props.rewardsTooltip && (
          <RewardsTooltip rewardsTooltip={props.rewardsTooltip} />
        )}
      </Flex>
    </Flex>
  );
};

const Statics: FC<{
  title: string;
  highLight?: string;
  text?: string;
}> = (props) => {
  const { title, highLight, text } = props;
  return (
    <Flex
      // px={1}
      py={2}
      justify={"between"}
      direction={"column"}
      className="flex-1"
      gap={2}
    >
      <Text
        className={cn(
          "oui-text-base-contrast-54",
          // font size
          "oui-text-xs md:oui-text-sm xl:oui-text-base",
          /// leading
          "oui-leading-[20px] xl:oui-leading-[24px]",
        )}
      >
        {title}
      </Text>
      <Flex direction={"row"} gap={1} itemAlign={"end"} justify={"center"}>
        <Text.gradient
          color="brand"
          angle={90}
          className="oui-text-base md:oui-text-lg lg:oui-text-xl xl:oui-text-2xl"
        >
          {highLight}
        </Text.gradient>
        <Text
          intensity={80}
          className="oui-mb-[3px] oui-text-2xs md:oui-text-xs xl:oui-text-sm"
        >
          {text}
        </Text>
      </Flex>
    </Flex>
  );
};

const Countdown: FC<{
  targetTimestamp?: number;
  isStandalone?: boolean;
}> = (props) => {
  const { targetTimestamp, isStandalone } = props;
  const { t } = useTranslation();
  const [timeLeft, setTimeLeft] = useState({
    days: 0,
    hours: 0,
    minutes: 0,
    seconds: 0,
  });

  useEffect(() => {
    const intervalId = setInterval(() => {
      if (typeof targetTimestamp === "undefined") return;
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

  const num = (value: string) => {
    return (
      <Text className="oui-text-sm oui-text-base-contrast md:oui-text-base lg:oui-text-lg ">
        {value}
      </Text>
    );
  };

  return (
    <Box
      className="oui-rounded-t-2xl oui-font-semibold oui-text-base-contrast-54"
      gradient={isStandalone ? undefined : "neutral"}
      angle={180}
      width={"full"}
    >
      <Flex justify={"center"} gap={1}>
        {isStandalone ? null : <span>{`${t("common.countdown")}: `}</span>}
        <Flex
          direction={"row"}
          itemAlign={"end"}
          gap={1}
          className={cn(
            "oui-text-2xs md:oui-text-xs lg:oui-text-sm",
            isStandalone ? "oui-py-0" : "oui-py-[13px]",
          )}
        >
          {num(`${timeLeft.days}`.padStart(2, "0"))}
          <span>D</span>
          {num(`${timeLeft.hours}`.padStart(2, "0"))}
          <span>H</span>
          {num(`${timeLeft.minutes}`.padStart(2, "0"))}
          <span>M</span>
          {num(`${timeLeft.seconds}`.padStart(2, "0"))}
          <span>S</span>
        </Flex>
      </Flex>
    </Box>
  );
};

const CountDownNum = (props: { children?: ReactNode }) => {
  return (
    <Text className="oui-text-sm oui-text-base-contrast md:oui-text-base lg:oui-text-lg ">
      {props.children}
    </Text>
  );
};

const getDate = (timestamp?: number) => {
  if (!timestamp) return "";

  const date = new Date(timestamp);

  // return format(timestamp, "MMM. d");

  const monthNames = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
  ];
  const month = monthNames[date.getUTCMonth()];
  const day = date.getUTCDate();
  return `${month}. ${day}`;
};

export const ArrowRightIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="21"
    height="20"
    viewBox="0 0 21 20"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <mask
      id="mask0_1997_45723"
      style={{ maskType: "alpha" }}
      maskUnits="userSpaceOnUse"
      x="0"
      y="0"
      width="21"
      height="20"
    >
      <rect
        x="0.5"
        width="20"
        height="20"
        fill="rgb(var(--oui-color-tertiary))"
      />
    </mask>
    <g mask="url(#mask0_1997_45723)">
      <path
        d="M12.0384 14.7111L11.1603 13.8073L14.343 10.6246H4.25V9.37463H14.343L11.1603 6.19192L12.0384 5.28809L16.75 9.99961L12.0384 14.7111Z"
        // fill="black"
        // fill-opacity="0.8"
      />
    </g>
  </svg>
);
const TwitterLInk: FC = () => {
  const { t } = useTranslation();
  return (
    <Flex
      gap={1}
      itemAlign={"center"}
      justify={"center"}
      className="oui-group oui-cursor-pointer oui-fill-base-contrast-36 oui-text-sm  oui-text-base-contrast-36 group-hover:oui-text-base-contrast-80"
      onClick={() => window.open("https://x.com/PaxeerNetworkNetwork")}
    >
      <div className="oui-cursor-pointer oui-fill-base-contrast-36 oui-text-sm oui-text-base-contrast-36  group-hover:oui-text-base-contrast-80">
        {t("tradingRewards.eopchStatus.linkDescription")}
      </div>
      <ArrowRightIcon className="oui-text-fill-base-contrast-36 oui-fill-base-contrast-36  group-hover:oui-fill-base-contrast-80" />
    </Flex>
  );
};
