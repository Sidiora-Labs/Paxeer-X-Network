import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Button,
  ButtonProps,
  cn,
  Flex,
  parseNumber,
  Text,
} from "@paxeer-network/ui";
import { useScaffoldContext } from "@paxeer-network/ui-scaffold";
import { Decimal } from "@paxeer-network/utils";
import { useReferralContext } from "../../../provider";

export type TradingVolumeProgressProps = {
  classNames?: {
    root?: string;
    description?: string;
    button?: string;
  };
  buttonProps?: ButtonProps;
};

export const TradingVolumeProgress: FC<TradingVolumeProgressProps> = (
  props,
) => {
  const { t } = useTranslation();
  const { classNames } = props;
  const { volumePrerequisite } = useReferralContext();
  const { routerAdapter } = useScaffoldContext();

  const { current_volume = 0, required_volume = 0 } = volumePrerequisite || {};

  const progressPercentage = useMemo(() => {
    if (required_volume === 0) return 0;
    const percentage = new Decimal(current_volume)
      .div(required_volume)
      .mul(100)
      .toNumber();
    return Math.min(percentage, 100);
  }, [current_volume, required_volume]);

  const formatRequiredVolume = useMemo(() => {
    return parseNumber(required_volume, {
      rule: "price",
      dp: 0,
    });
  }, [required_volume]);

  const gotoTrade = () => {
    routerAdapter?.onRouteChange?.({
      href: "/perp",
      name: "Perp",
    });
  };

  return (
    <Flex
      width="100%"
      direction="column"
      gap={4}
      className={cn("oui-tradingVolumeProgress", classNames?.root)}
    >
      <Text
        size="sm"
        intensity={54}
        className={cn(
          "oui-tradingVolumeProgress-description",
          "oui-text-center",
          classNames?.description,
        )}
      >
        {t("affiliate.newReferralProgram.tradeUnlock.description", {
          volume: formatRequiredVolume,
        })}
      </Text>
      <Flex width="100%" direction={"column"} gap={2}>
        <Flex
          width="100%"
          justify="between"
          className="oui-text-2xs oui-text-base-contrast"
        >
          <Text>{t("common.current")}</Text>
          <Flex gap={1}>
            <Text.numeral rule="price" dp={0}>
              {current_volume}
            </Text.numeral>
            <Text.numeral
              intensity={54}
              rule="price"
              dp={0}
              prefix={"/ "}
              suffix=" USDC"
            >
              {required_volume}
            </Text.numeral>
          </Flex>
        </Flex>

        <div className="oui-h-[8px] oui-w-full oui-rounded-full oui-bg-base-contrast-4">
          <div
            style={{
              width: `${progressPercentage}%`,
            }}
            className={cn(
              "oui-tradingVolumeProgress-bar oui-h-full oui-rounded-s-full oui-bg-primary-light",
              progressPercentage === 100 && "oui-rounded-e-full",
            )}
          />
        </div>
      </Flex>

      <Button
        size="md"
        onClick={gotoTrade}
        className={cn(
          "oui-tradingVolumeProgress-cta-btn",
          props.buttonProps?.className,
        )}
        {...props.buttonProps}
      >
        {t("affiliate.newReferralProgram.tradeUnlock")}
      </Button>
    </Flex>
  );
};
