import { FC } from "react";
import { RefferalAPI as API } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import type { RouterAdapter } from "@paxeer-network/types";
import {
  Flex,
  Text,
  AffiliateIcon,
  ChevronRightIcon,
  cn,
} from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";

const isNumber = (val: unknown): val is number => {
  return typeof val === "number" && !Number.isNaN(val);
};

type AffiliateCardMobileProps = {
  referralInfo?: API.ReferralInfo;
  routerAdapter?: RouterAdapter;
};

export const AffiliateCardMobile: FC<AffiliateCardMobileProps> = (props) => {
  const { t } = useTranslation();
  const { referralInfo, routerAdapter } = props;
  const rebate = referralInfo?.referrer_info?.["30d_referrer_rebate"];
  return (
    <Flex
      className="
        oui-portfolio-affiliate-card
        oui-via-21.6% 
        oui-via-83.23% 
        oui-relative 
        oui-h-[112px] 
        oui-w-full 
        oui-flex-col 
        oui-items-start 
        oui-rounded-xl 
        oui-border 
        oui-border-solid 
        oui-border-[rgba(var(--oui-gradient-primary-start)/0.36)] 
        oui-bg-gradient-to-r
        oui-from-[rgba(var(--oui-gradient-primary-end)/0.12)]
        oui-to-[rgba(var(--oui-gradient-primary-start)/0.12)]
        oui-p-3
      "
    >
      <Flex className="oui-w-full oui-flex-row oui-justify-between oui-items-center">
        <Flex className="oui-flex-col oui-items-start">
          <Text className="oui-text-base-contrast oui-text-base oui-font-semibold">
            {t("affiliate.asAffiliate.affilates")}
          </Text>
          <Text className="oui-text-2xs oui-font-normal oui-text-base-contrast-54">
            {t("affiliate.commission")}
          </Text>
        </Flex>
        <AffiliateIcon />
      </Flex>
      <Flex className="oui-mt-auto oui-w-full oui-flex-row oui-items-center oui-gap-1">
        <img
          src="/assets/static/symbol_logo/USDC.png"
          alt="USDC"
          className="oui-size-5"
        />
        <Text
          className={cn(
            "oui-text-xs oui-font-semibold",
            isNumber(rebate) && rebate !== 0
              ? "oui-text-base-contrast"
              : "oui-text-base-contrast-36",
          )}
        >
          {commifyOptional(rebate, { fix: 2, fallback: "--" })}
        </Text>
        <Text className="oui-text-xs oui-font-semibold oui-text-base-contrast-36">
          (30D)
        </Text>
        <ChevronRightIcon
          size={18}
          color="white"
          className="oui-ms-auto"
          onClick={() => {
            routerAdapter?.onRouteChange({
              href: "/rewards/affiliate?tab=affiliate",
              name: t("tradingRewards.rewards"),
            });
          }}
        />
      </Flex>
    </Flex>
  );
};
