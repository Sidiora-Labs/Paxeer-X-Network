import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, cn, Flex, Text } from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";
import { ArrowRightIcon } from "../../../icons/arrowRightIcon";
import { USDCIcon } from "../../../icons/usdcIcon";
import { AsAnAffiliateReturns } from "./asAnAffiliate.script";

export const AsAnAffiliate: FC<AsAnAffiliateReturns> = (props) => {
  const { t } = useTranslation();
  return (
    <Flex
      id="oui-affiliate-home-asAnAffiliate"
      r={"2xl"}
      p={6}
      gap={6}
      direction={"column"}
      width={"100%"}
      justify={"between"}
      className="oui-bg-primary-darken oui-text-primary-contrast"
    >
      <Flex
        direction={"row"}
        gap={3}
        itemAlign={"start"}
        width={"100%"}
        justify={"between"}
      >
        <Flex
          direction={"column"}
          itemAlign={"start"}
          justify={"between"}
          className="oui-h-full"
        >
          <Text className="oui-text-lg md:oui-text-xl lg:oui-text-2xl xl:oui-text-3xl">
            {props.isAffiliate
              ? t("common.affiliate")
              : t("affiliate.asAffiliate.title")}
          </Text>
          <Text
            className={cn(
              "oui-text-xs oui-text-base-contrast-54 md:oui-text-sm 2xl:oui-text-base",
              props.isAffiliate && "oui-hidden",
            )}
          >
            {t("affiliate.asAffiliate.description")}
          </Text>
        </Flex>
        <div className="oui-shrink-0">
          <Icon />
        </div>
      </Flex>
      <Bottom {...props} />
    </Flex>
  );
};

const Icon = () => {
  return (
    <svg
      width="72"
      height="72"
      viewBox="0 0 72 72"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="xl:oui-size-[90px]"
    >
      <path
        d="M35.997 5.86c-16.569 0-29.944 13.43-30 30-.056 16.545 13.445 30.06 30 30.093s30.03-13.665 30-30.093c-.03-16.57-13.432-30-30-30m0 6c13.255 0 24 10.746 24 24 0 6.75-2.812 12.834-7.297 17.196-2.209-4.78-6.955-8.196-12.39-8.196h-8.626c-5.432 0-10.164 3.375-12.375 8.157-4.485-4.362-7.312-10.407-7.312-17.157 0-13.254 10.745-24 24-24m0 6c-6.628 0-12 5.373-12 12s5.372 12 12 12c6.627 0 12-5.373 12-12s-5.373-12-12-12"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".2"
      />
      <path
        d="M72 57c0 8.284-6.716 15-15 15s-15-6.716-15-15 6.716-15 15-15 15 6.716 15 15"
        fill="rgb(var(--oui-gradient-primary-end))"
      />
      <path
        d="M53.624 51.45a4.125 4.125 0 0 0-4.125 4.125c0 2.388 1.36 4.641 3.61 6.703a21 21 0 0 0 2.414 1.875c.287.194.556.37.797.516.147.089.243.154.304.187a.79.79 0 0 0 .75 0c.061-.033.158-.098.305-.187.24-.147.51-.322.797-.516a21 21 0 0 0 2.414-1.875c2.25-2.062 3.61-4.315 3.61-6.703a4.125 4.125 0 0 0-4.126-4.125c-1.294 0-2.557.705-3.351 1.734-.775-1.047-2.034-1.734-3.399-1.734"
        fill="rgb(var(--oui-color-base-foreground))"
        fillOpacity=".98"
      />
    </svg>
  );
};

const Bottom: FC<AsAnAffiliateReturns> = (props) => {
  const { t } = useTranslation();

  const content = () => {
    if (props.isAffiliate && !props.wrongNetwork) {
      const totalReferrerRebate =
        props.referralInfo?.referrer_info?.total_referrer_rebate;
      return (
        <>
          <Flex direction={"column"} itemAlign={"start"} gap={2}>
            <Text className="oui-text-2xs md:oui-text-xs xl:oui-text-sm">
              {t("affiliate.commission")} {<Text intensity={36}>(USDC)</Text>}
            </Text>
            <Flex direction={"row"} gap={1}>
              <USDCIcon />
              <Text className="oui-text-base md:oui-text-lg lg:oui-text-xl xl:oui-text-2xl">
                {commifyOptional(totalReferrerRebate, {
                  fix: 2,
                  fallback: "0",
                })}
              </Text>
            </Flex>
          </Flex>

          <Flex
            direction={"row"}
            gap={1}
            justify={"end"}
            itemAlign={"center"}
            className="oui-cursor-pointer"
            onClick={props.onEnterAffiliatePage}
          >
            <Text className="oui-text-sm md:oui-text-base xl:oui-text-lg">
              {t("affiliate.enter")}
            </Text>
            <ArrowRightIcon className="md:oui-size-[18px] lg:oui-size-[20px] xl:oui-size-[24px]" />
          </Flex>
        </>
      );
    }

    return (
      <>
        <Button
          variant="contained"
          color="light"
          onClick={props.becomeAnAffiliate}
        >
          {t("affiliate.asAffiliate.button")}
        </Button>
        <Flex
          direction={"column"}
          justify={"between"}
          className="oui-h-full"
          itemAlign={"end"}
        >
          <Text className="oui-text-base md:oui-text-lg lg:oui-text-xl 2xl:oui-text-2xl">
            {t("affiliate.upTo")}
          </Text>
          <Text className="oui-text-2xs oui-text-base-contrast-54 md:oui-text-xs 2xl:oui-text-sm">
            {t("affiliate.commission")}
          </Text>
        </Flex>
      </>
    );
  };

  return (
    <Flex
      direction={"row"}
      justify={"between"}
      width={"100%"}
      itemAlign={"end"}
    >
      {content()}
    </Flex>
  );
};
