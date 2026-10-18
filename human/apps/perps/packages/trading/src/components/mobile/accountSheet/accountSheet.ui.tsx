import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ArrowRightShortIcon,
  Button,
  cn,
  Flex,
  Text,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { AccountSheetState } from "./accountSheet.script";
import { CopyIcon, HeadIcon, PaxeerNetworkIcon, USDCIcon } from "./icons";

export const AccountSheet: FC<AccountSheetState> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex className="oui-accountSheet" direction={"column"} gap={4}>
      <AccountInfo {...props} />
      <ReferralInfo {...props} />
      <TradingRewardsInfo {...props} />
      <Flex gap={3} width={"100%"} justify={"center"}>
        {props.showGetTestUSDC && (
          <Button
            variant="outlined"
            // color="primary"
            size="md"
            onClick={props.onGetTestUSDC}
            fullWidth
            loading={props.gettingTestUSDC}
            className="oui-accountSheet-getTestUSDC-btn oui-border-primary-light oui-text-primary-light"
          >
            {t("trading.faucet.getTestUSDC")}
          </Button>
        )}
        <Button
          variant="outlined"
          color="danger"
          size="md"
          onClick={props.onDisconnect}
          className={props.showGetTestUSDC ? "oui-w-full" : "oui-w-[50%]"}
        >
          {t("connector.disconnect")}
        </Button>
      </Flex>
    </Flex>
  );
};
export const AccountInfo: FC<AccountSheetState> = (props) => {
  return (
    <Flex width={"100%"} justify={"between"}>
      <Flex gap={3}>
        <HeadIcon />
        <Flex
          direction={"column"}
          justify={"start"}
          itemAlign={"start"}
          className="oui-gap-[2px]"
        >
          <Text.formatted rule={"address"}>{props.address}</Text.formatted>
          <Text.formatted
            size="2xs"
            intensity={80}
            // @ts-ignore
            prefix={
              <div className="oui-h-1 oui-w-1 oui-rounded-full oui-bg-success oui-pe-1" />
            }
          >
            {props.chainName}
          </Text.formatted>
        </Flex>
      </Flex>
      <button
        className="oui-cursor-pointer"
        onClick={() => {
          props.onCopyAddress();
        }}
      >
        <CopyIcon />
      </button>
    </Flex>
  );
};

export const ReferralInfo: FC<AccountSheetState> = (props) => {
  const { t } = useTranslation();
  return (
    <Flex
      direction={"column"}
      gap={3}
      r="lg"
      p={3}
      className="oui-bg-base-6"
      width={"100%"}
    >
      <button
        onClick={props.onClickReferral}
        className="oui-cursor-pointer oui-w-full"
      >
        <Flex justify={"between"} width={"100%"}>
          <Text size="2xs">{t("affiliate.referral")}</Text>
          <ArrowRightShortIcon color="white" opacity={0.98} size={16} />
        </Flex>
      </button>
      {props.isAffiliate && (
        <Flex
          gradient="primary"
          p={4}
          r="lg"
          width={"100%"}
          angle={180}
          justify={"between"}
        >
          <Flex direction={"column"} itemAlign={"start"}>
            <Text size="xs">{t("common.affiliate")}</Text>
            <Text size="2xs" intensity={54}>
              ({t("affiliate.commission.30d")})
            </Text>
          </Flex>
          <Flex className="oui-gap-[6px]">
            <USDCIcon />
            <Text.numeral
              dp={2}
              padding={false}
              rm={Decimal.ROUND_DOWN}
              rule="price"
            >
              {props.affiliateCommission30D ?? "--"}
            </Text.numeral>
          </Flex>
        </Flex>
      )}
      {props.isTrader && (
        <Flex
          gradient="success"
          p={4}
          r="lg"
          width={"100%"}
          angle={180}
          justify={"between"}
        >
          <Flex direction={"column"} itemAlign={"start"}>
            <Text size="xs">{t("affiliate.trader")}</Text>
            <Text size="2xs" intensity={54}>
              ({t("affiliate.commission.30d")})
            </Text>
          </Flex>
          <Flex className="oui-gap-[6px]">
            <USDCIcon />
            <Text.numeral
              dp={2}
              padding={false}
              rm={Decimal.ROUND_DOWN}
              rule="price"
            >
              {props.traderCommission30D ?? "--"}
            </Text.numeral>
          </Flex>
        </Flex>
      )}
    </Flex>
  );
};

export const TradingRewardsInfo: FC<AccountSheetState> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      direction={"column"}
      gap={3}
      r="lg"
      p={3}
      className="oui-bg-base-6"
      width={"100%"}
    >
      <button
        onClick={props.onClickTradingRewards}
        className="oui-cursor-pointer oui-w-full"
      >
        <Flex justify={"between"} width={"100%"}>
          <Flex gap={1} className="oui-text-base-contrast oui-text-2xs">
            <Text>{t("common.tradingRewards")}</Text>
            <Text intensity={54}>
              (
              <Text>{`${(
                t("tradingRewards.epoch") as string
              )?.toLowerCase()} `}</Text>
              <Text intensity={98}>{props.curEpochId}</Text> )
            </Text>
          </Flex>
          <ArrowRightShortIcon color="white" opacity={0.98} size={16} />
        </Flex>
      </button>
      <Flex
        p={4}
        r="lg"
        width={"100%"}
        angle={180}
        justify={"between"}
        className="oui-bg-gradient-to-t oui-from-[rgb(var(--oui-gradient-secondary-end))] oui-to-[rgb(var(--oui-gradient-secondary-start))]"
      >
        <Text size="2xs" intensity={80}>
          {t("tradingRewards.myEstRewards")}
        </Text>

        <Flex className="oui-gap-[6px]">
          <PaxeerNetworkIcon />
          <Text.numeral
            dp={2}
            padding={false}
            rm={Decimal.ROUND_DOWN}
            rule="price"
          >
            {props.estRewards}
          </Text.numeral>
        </Flex>
      </Flex>
    </Flex>
  );
};
