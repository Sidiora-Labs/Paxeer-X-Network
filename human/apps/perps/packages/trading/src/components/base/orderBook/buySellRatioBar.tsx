import React, { FC, useMemo } from "react";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { BuySellRatio } from "./orderBook.script";

export interface BuySellRatioBarProps {
  ratio: BuySellRatio | null;
  className?: string;
}

export const BuySellRatioBar: FC<BuySellRatioBarProps> = (props) => {
  const { ratio, className } = props;

  const { buyPercentage, sellPercentage } = useMemo(() => {
    if (!ratio) {
      return { buyPercentage: 50, sellPercentage: 50 };
    }

    // Validate that percentages are valid numbers
    const isValidNumber = (value: number) => {
      return (
        typeof value === "number" &&
        !Number.isNaN(value) &&
        Number.isFinite(value) &&
        value >= 0 &&
        value <= 100
      );
    };

    const buyPct = isValidNumber(ratio.buyPercentage)
      ? ratio.buyPercentage
      : 50;

    // Round to 1 decimal place
    const buyPctRounded = new Decimal(buyPct)
      .toDecimalPlaces(1, Decimal.ROUND_HALF_UP)
      .toNumber();

    // Ensure percentages add up to 100% to avoid display issues
    const sellPctRounded = new Decimal(100)
      .sub(buyPctRounded)
      .toDecimalPlaces(1, Decimal.ROUND_HALF_UP)
      .toNumber();

    return {
      buyPercentage: buyPctRounded,
      sellPercentage: sellPctRounded,
    };
  }, [ratio]);

  return (
    <Flex className={cn("oui-w-full", className)} gap={1}>
      <Flex itemAlign="center" gap={1}>
        <Text intensity={80}>B</Text>
        <Text color="success">{buyPercentage.toFixed(1)}%</Text>
      </Flex>
      <div
        style={{
          flex: 1,
          height: "4px",
          position: "relative",
          borderRadius: "2px",
          overflow: "hidden",
        }}
        className="oui-bg-line-4"
      >
        <div
          className="oui-bg-trade-profit oui-me-[2px] oui-rounded-sm"
          style={{
            position: "absolute",
            insetInlineStart: 0,
            top: 0,
            width: `${buyPercentage}%`,
            height: "100%",
          }}
        />
        <div
          className="oui-bg-trade-loss oui-ms-[2px] oui-rounded-sm"
          style={{
            position: "absolute",
            insetInlineStart: `${buyPercentage}%`,
            top: 0,
            width: `${sellPercentage}%`,
            height: "100%",
          }}
        />
      </div>
      <Flex itemAlign="center" gap={1}>
        <Text color="danger">{sellPercentage.toFixed(1)}%</Text>
        <Text intensity={80}>S</Text>
      </Flex>
    </Flex>
  );
};
