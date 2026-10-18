import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Checkbox,
  Divider,
  Flex,
  Statistic,
  Text,
  useScreen,
} from "@paxeer-network/ui";
import { CloseAllPositionsWidget } from "@paxeer-network/ui-positions";
import { Decimal } from "@paxeer-network/utils";
import { SettingWidget } from "../../desktop/dataList/setting";
import type { PositionHeaderState } from "./positionHeader.script";

export const PositionHeader: React.FC<PositionHeaderState> = (props) => {
  const { isMobile } = useScreen();
  return isMobile ? <MobileLayout {...props} /> : <DesktopLayout {...props} />;
};

const MobileLayout: React.FC<PositionHeaderState> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex
      className="oui-positionHeader oui-rounded-b-xl oui-bg-base-9"
      direction={"column"}
      gap={2}
      width={"100%"}
      itemAlign={"start"}
      p={2}
    >
      <Flex width={"100%"} justify={"between"} gap={2}>
        <UnrealPnL
          classNames={{
            label: "oui-text-2xs oui-text-base-contrast-54",
            root: "oui-text-sm",
          }}
          {...props}
        />
        <Notional
          classNames={{
            label: "oui-text-2xs oui-text-base-contrast-54",
            root: "oui-text-sm",
          }}
          {...props}
        />
      </Flex>
      <Divider className="oui-w-full" />
      <Flex
        className="oui-cursor-pointer oui-gap-[2px]"
        justify={"between"}
        width={"100%"}
      >
        <Flex>
          <SettingWidget
            pnlNotionalDecimalPrecision={props.pnlNotionalDecimalPrecision}
            setPnlNotionalDecimalPrecision={
              props.setPnlNotionalDecimalPrecision
            }
            unPnlPriceBasis={props.unPnlPriceBasis}
            setUnPnlPriceBasic={props.setUnPnlPriceBasic}
            hideOtherSymbols={!props.showAllSymbol}
            setHideOtherSymbols={(value: boolean) => {
              props.setShowAllSymbol(!value);
            }}
          />
        </Flex>
        <CloseAllPositionsWidget symbol={props.symbol} />
      </Flex>
    </Flex>
  );
};

const DesktopLayout: React.FC<PositionHeaderState> = (props) => {
  return (
    <Flex
      className="oui-positionHeader"
      py={2}
      px={3}
      gap={6}
      width={"100%"}
      justify={"between"}
    >
      <Flex gap={5}>
        <UnrealPnL
          {...props}
          classNames={{ label: "oui-text-base-contrast-54" }}
        />
        <Notional
          {...props}
          classNames={{ label: "oui-text-base-contrast-54" }}
        />
      </Flex>

      <CloseAllPositionsWidget symbol={props.symbol} />
    </Flex>
  );
};

const UnrealPnL: React.FC<
  PositionHeaderState & {
    classNames?: { root?: string; label?: string; value?: string };
  }
> = (props) => {
  const { t } = useTranslation();

  const unrealPnLClsName =
    typeof props.unrealPnL === "number"
      ? props.unrealPnL >= 0
        ? "oui-text-trade-profit"
        : "oui-text-trade-loss"
      : "oui-text-base-contrast-80";

  const unrealPnLROIClsName =
    typeof props.unrealPnL === "number" && props.unrealPnlROI
      ? props.unrealPnlROI >= 0
        ? "oui-text-success-darken"
        : "oui-text-danger-darken"
      : "oui-text-base-contrast-80";

  return (
    <Statistic label={t("common.unrealizedPnl")} classNames={props.classNames}>
      <Flex gap={1}>
        <Text.pnl
          dp={props.pnlNotionalDecimalPrecision}
          intensity={80}
          className={unrealPnLClsName}
        >
          {props.unrealPnL ?? "--"}
        </Text.pnl>
        {typeof props.unrealPnlROI !== "undefined" && (
          <Text.roi
            prefix="("
            suffix=")"
            rule="percentages"
            size="2xs"
            dp={props.pnlNotionalDecimalPrecision}
            className={unrealPnLROIClsName}
          >
            {props.unrealPnlROI}
          </Text.roi>
        )}
      </Flex>
    </Statistic>
  );
};

const Notional: React.FC<
  PositionHeaderState & {
    classNames?: { root?: string; label?: string; value?: string };
  }
> = (props) => {
  const { t } = useTranslation();
  return (
    <Statistic label={t("common.notional")} classNames={props.classNames}>
      <Text.numeral
        dp={props.pnlNotionalDecimalPrecision}
        rm={Decimal.ROUND_DOWN}
        intensity={80}
      >
        {props.notional ?? "--"}
      </Text.numeral>
    </Statistic>
  );
};
