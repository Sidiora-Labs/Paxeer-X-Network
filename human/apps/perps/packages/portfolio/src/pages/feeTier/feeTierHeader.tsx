/* eslint-disable @typescript-eslint/no-explicit-any */
import { useMemo } from "react";
import { useFeeState } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex, Text, useScreen } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { EffectiveFee } from "./icons";

export type FeeTierHeaderItemProps = {
  label: string;
  value: React.ReactNode;
  interactive?: boolean;
};

export type FeeTierHeaderProps = {
  tier?: number | null;
  vol?: number | null;
  headerDataAdapter?: (original: any[]) => any[];
};

export const MobileHeaderItem: React.FC<FeeTierHeaderItemProps> = (props) => {
  const { label, value, interactive } = props;
  return (
    <Flex justify="between" itemAlign="center" width="100%">
      <Text
        as="div"
        intensity={36}
        size="xs"
        weight="semibold"
        className="oui-leading-[18px]"
      >
        {label}
      </Text>
      <Flex className="oui-gap-1.5" itemAlign="center" justify="between">
        <Text size="xs" intensity={80} className="oui-leading-[24px]">
          {value}
        </Text>
        {interactive && <EffectiveFee />}
      </Flex>
    </Flex>
  );
};

export const DesktopHeaderItem: React.FC<FeeTierHeaderItemProps> = (props) => {
  const { label, value, interactive } = props;
  return (
    <Box gradient="neutral" r="lg" px={4} py={2} angle={184} width="100%">
      <Flex itemAlign="center" justify="between">
        <Text
          as="div"
          intensity={36}
          size="2xs"
          weight="semibold"
          className="oui-leading-[18px]"
        >
          {label}
        </Text>
        {interactive && <EffectiveFee />}
      </Flex>
      <Flex className="oui-mt-1 oui-w-full">
        <Text size="base" intensity={80} className="oui-leading-[24px]">
          {value}
        </Text>
      </Flex>
    </Box>
  );
};

export const FeeTierHeader: React.FC<FeeTierHeaderProps> = (props) => {
  const { t } = useTranslation();
  const { tier, vol, headerDataAdapter } = props;
  const { isMobile } = useScreen();
  const { takerFee, makerFee, rwaTakerFee, rwaMakerFee } = useFeeState();

  const mergedData = useMemo<FeeTierHeaderItemProps[]>(() => {
    const baseItems: FeeTierHeaderItemProps[] = [
      {
        label: t("portfolio.feeTier.header.yourTier"),
        interactive: false,
        value: (
          <Text.gradient
            color={"brand"}
            angle={270}
            size={isMobile ? "xs" : "base"}
          >
            {tier || "--"}
          </Text.gradient>
        ),
      },
      {
        label: `${t("portfolio.feeTier.header.30dVolume")} (USDC)`,
        interactive: false,
        value: (
          <Text.numeral
            rule="price"
            dp={2}
            rm={Decimal.ROUND_DOWN}
            size={isMobile ? "xs" : "base"}
          >
            {vol !== undefined && vol !== null ? `${vol}` : "-"}
          </Text.numeral>
        ),
      },
      {
        label: t("portfolio.feeTier.header.takerFeeRate"),
        interactive: false,
        value: (
          <Text.gradient
            color={"brand"}
            angle={270}
            size={isMobile ? "xs" : "base"}
          >
            {takerFee || "--"} (RWA: {rwaTakerFee || "--"})
          </Text.gradient>
        ),
      },
      {
        label: t("portfolio.feeTier.header.makerFeeRate"),
        interactive: false,
        value: (
          <Text.gradient
            color={"brand"}
            angle={270}
            size={isMobile ? "xs" : "base"}
          >
            {makerFee || "--"} (RWA: {rwaMakerFee || "--"})
          </Text.gradient>
        ),
      },
    ];

    if (typeof headerDataAdapter === "function") {
      return headerDataAdapter(baseItems);
    }
    return baseItems;
  }, [
    headerDataAdapter,
    isMobile,
    makerFee,
    rwaMakerFee,
    rwaTakerFee,
    t,
    takerFee,
    tier,
    vol,
  ]);

  if (!Array.isArray(mergedData)) {
    return null;
  }

  if (isMobile) {
    return (
      <Flex
        className="oui-rounded-xl oui-bg-base-9 oui-p-3"
        direction="column"
        gap={2}
        itemAlign={"stretch"}
      >
        {mergedData.map((item, index) => (
          <MobileHeaderItem {...item} key={`mobile-item-${index}`} />
        ))}
      </Flex>
    );
  }

  return (
    <Flex className="" direction="row" gapX={4} my={4} itemAlign={"stretch"}>
      {mergedData.map((item, index) => (
        <DesktopHeaderItem {...item} key={`desktop-item-${index}`} />
      ))}
    </Flex>
  );
};
