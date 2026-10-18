import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Text, Divider, Badge } from "@paxeer-network/ui";
import { commifyOptional, Decimal } from "@paxeer-network/utils";
import { SymbolBadge } from "../../positions/desktop/symbolBadge";
import { LiquidationCellState } from "./liquidationCell.script";

export const LiquidationCell: FC<
  LiquidationCellState & {
    classNames?: {
      root?: string;
    };
  }
> = (props) => {
  return (
    <Flex
      key={props.item.timestamp}
      direction={"column"}
      width={"100%"}
      gap={2}
      itemAlign={"start"}
      className={cn(
        "oui-rounded-xl oui-bg-base-9 oui-p-2",
        props.classNames?.root,
      )}
    >
      <Header {...props} />
      <Divider />
      <Body {...props} />
    </Flex>
  );
};

export const Header: FC<LiquidationCellState> = (props) => {
  const { t } = useTranslation();
  return (
    <Flex width={"100%"} justify={"between"} itemAlign={"start"}>
      <Flex direction={"column"} itemAlign={"start"} gap={1}>
        <Flex gap={2} itemAlign={"center"}>
          <Flex direction={"column"} gap={1} itemAlign={"start"}>
            <Text.formatted
              rule={"symbol"}
              formatString="base"
              size="xs"
              intensity={80}
              suffix={
                <SymbolBadge
                  symbol={props.item.positions_by_perp?.[0]?.symbol || ""}
                />
              }
            >
              {props.item.positions_by_perp?.[0]?.symbol || ""}
            </Text.formatted>
            {/* <Badge size="sm" color="neutral">
              Cross 10X
            </Badge> */}
          </Flex>
        </Flex>
      </Flex>
      <Flex direction={"column"} itemAlign={"end"}>
        <Text size="2xs" intensity={36}>
          <Text.formatted rule={"date"} formatString="yyyy-MM-dd HH:mm:ss">
            {props.item.timestamp}
          </Text.formatted>
        </Text>
        <Text size="2xs" intensity={36}>
          {`${t("positions.Liquidation.column.liquidationId")}: `}
          <Text as="span" intensity={80}>
            {props.item.liquidation_id}
          </Text>
        </Text>
      </Flex>
    </Flex>
  );
};

export const Body: FC<LiquidationCellState> = (props) => {
  const position = props.item.positions_by_perp?.[0];
  const { t } = useTranslation();
  const mr = useMemo(() => {
    if (isNaN(props.item?.margin_ratio)) return "--";
    return `${new Decimal(props.item.margin_ratio).mul(100).todp(2, Decimal.ROUND_DOWN).toNumber()}%`;
  }, [props.item?.margin_ratio]);

  if (!position) return null;

  return (
    <Flex direction={"column"} width={"100%"} gap={1}>
      {/* First row */}
      <Flex gap={2} width={"100%"}>
        <DataItem
          label={t("positions.Liquidation.column.markPrice")}
          value={commifyOptional(position.transfer_price)}
          className="oui-flex-1"
        />
        <DataItem
          label={t("common.quantity")}
          value={commifyOptional(position.position_qty)}
          className="oui-flex-1"
        />
        <DataItem
          label={t("positions.Liquidation.column.liquidationFeeRate")}
          value={props.item.liquidationFeeRate!}
          className="oui-flex-1"
          align="end"
        />
      </Flex>

      {/* Second row */}
      <Flex gap={2} width={"100%"}>
        <DataItem
          label={t("positions.Liquidation.column.liquidationFee")}
          value={commifyOptional(position.abs_liquidation_fee)}
          className="oui-flex-1"
        />
        <DataItem
          label={t("positions.Liquidation.expand.label.mr")}
          value={mr}
          className="oui-flex-1"
        />
        <DataItem
          label={t("positions.Liquidation.expand.label.mmr")}
          value={
            props.item.formatted_account_mmr
              ? `${props.item.formatted_account_mmr}%`
              : "--"
          }
          className="oui-flex-1"
          align="end"
        />
      </Flex>

      {/* Third row */}
      <Flex gap={2} width={"100%"}>
        <DataItem
          label={t("positions.Liquidation.expand.label.collateral")}
          value={commifyOptional(props.item.collateral_value)}
          className="oui-flex-1"
        />
        <DataItem
          label={t("positions.Liquidation.expand.label.notional")}
          value={commifyOptional(props.item.position_notional)}
          className="oui-flex-1"
        />
        <div className="oui-flex-1 oui-opacity-0" /> {/* Empty placeholder */}
      </Flex>
    </Flex>
  );
};

const DataItem: FC<{
  label: string;
  value: string | number;
  className?: string;
  align?: "start" | "end";
}> = ({ label, value, className, align = "start" }) => {
  return (
    <Flex direction={"column"} itemAlign={align} className={className}>
      <Text size="2xs" intensity={36}>
        {label}
      </Text>
      <Text size="xs" intensity={80}>
        {value}
      </Text>
    </Flex>
  );
};
