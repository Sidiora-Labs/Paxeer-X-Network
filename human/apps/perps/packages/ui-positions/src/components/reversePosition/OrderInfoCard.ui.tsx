import React, { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide } from "@paxeer-network/types";
import { Badge, Flex, Text } from "@paxeer-network/ui";

export interface OrderInfoCardProps {
  /** Order title */
  title: string;
  /** Order side */
  side: OrderSide;
  /** Leverage multiplier */
  leverage: number;
  /** Quantity */
  qty: string;
  /** Base decimal places */
  baseDp: number;
  /** Price label, defaults to "Market" */
  estLiqPrice?: React.ReactNode;
  /** Estimated PnL value */
  estPnL?: string;
  /** PnL decimal precision */
  pnlNotionalDecimalPrecision?: number;
  /** Custom class name */
  className?: string;
}

/**
 * Order info card component
 * Displays detailed order information including title, side, leverage, quantity, and price
 */
export const OrderInfoCard: FC<OrderInfoCardProps> = ({
  title,
  side,
  leverage,
  qty,
  baseDp,
  estLiqPrice,
  estPnL,
  pnlNotionalDecimalPrecision = 2,
  className,
}) => {
  const { t } = useTranslation();

  return (
    <Flex
      direction="column"
      gap={3}
      itemAlign="start"
      className={`oui-bg-base-6 oui-rounded-lg oui-p-3 oui-w-full oui-font-weight-semibold ${className || ""}`}
    >
      <Flex justify="between" itemAlign="center" gap={2}>
        <Text size="sm" weight="semibold" intensity={98}>
          {title}
        </Text>
        <Flex itemAlign="center" gap={1}>
          <Badge color={side === OrderSide.SELL ? "sell" : "buy"} size="xs">
            {side === OrderSide.SELL ? t("common.sell") : t("common.buy")}
          </Badge>
          <Badge color="neutral" size="xs">
            {leverage}X
          </Badge>
        </Flex>
      </Flex>

      {/* Order Info */}
      <Flex
        direction="column"
        justify="between"
        itemAlign="center"
        width="100%"
        gap={1}
      >
        <Flex
          direction="row"
          justify="between"
          itemAlign="center"
          width="100%"
          gap={1}
        >
          <Text size="sm" intensity={54}>
            {t("common.qty")}
          </Text>
          <Text.numeral dp={baseDp} size="sm" color="danger" padding={false}>
            {qty}
          </Text.numeral>
        </Flex>
        <Flex
          direction="row"
          justify="between"
          itemAlign="center"
          width="100%"
          gap={1}
        >
          <Text size="sm" intensity={54}>
            {t("common.price")}
          </Text>
          <Text size="sm" weight="semibold">
            {t("common.market")}
          </Text>
        </Flex>
        {estPnL && (
          <Flex
            direction="row"
            justify="between"
            itemAlign="center"
            width="100%"
            gap={1}
          >
            <Text size="sm" intensity={54}>
              {t("orderEntry.estPnL")}
            </Text>
            <Text.pnl
              dp={pnlNotionalDecimalPrecision}
              coloring
              size="sm"
              weight="semibold"
            >
              {estPnL}
            </Text.pnl>
          </Flex>
        )}
        {estLiqPrice && (
          <Flex
            direction="row"
            justify="between"
            itemAlign="center"
            width="100%"
            gap={1}
          >
            <Text size="sm" intensity={54}>
              {t("orderEntry.estLiqPrice")}
            </Text>
            <Text size="sm" weight="semibold">
              {estLiqPrice}
            </Text>
          </Flex>
        )}
      </Flex>
    </Flex>
  );
};
