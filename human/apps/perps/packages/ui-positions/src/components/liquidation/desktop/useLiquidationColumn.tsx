import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Column, Flex, Text, Tooltip, cn } from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";
import { PositionSymbolCell } from "../../positions/desktop/positionSymbolCell";

const TooltipButton: FC<{
  tooltip: string;
  label: string;
}> = (props) => {
  return (
    <Tooltip
      className="oui-w-[275px] oui-bg-base-6"
      content={props.tooltip}
      arrow={{
        className: "oui-fill-base-6",
      }}
    >
      <button className="oui-border-b oui-border-dashed oui-border-line-12">
        {props.label}
      </button>
    </Tooltip>
  );
};

export const useLiquidationColumn = (props: {}) => {
  const { t } = useTranslation();

  const column = useMemo(
    () =>
      [
        // Time
        {
          title: t("common.time"),
          dataIndex: "timestamp",
          width: 202,
          render: (value: string) => (
            <Text.formatted rule={"date"} formatString="yyyy-MM-dd HH:mm:ss">
              {value}
            </Text.formatted>
          ),
        },
        // Liquidation id
        {
          title: t("positions.Liquidation.column.liquidationId"),
          dataIndex: "liquidation_id",
          width: 120,
          render: (value) => <Text>{value}</Text>,
        },
        // Symbol
        {
          title: t("common.symbol"),
          dataIndex: "Symbol",
          width: 120,
          render: (_: any, record) => (
            <Flex direction={"column"} itemAlign={"start"}>
              {record.positions_by_perp?.map((item) => (
                <PositionSymbolCell key={item.symbol} symbol={item.symbol} />
              ))}
            </Flex>
          ),
        },
        // Price (USDC)
        {
          title: t("positions.Liquidation.column.markPrice"),
          dataIndex: "Price_(USDC)",
          // width: 202,
          render: (_: any, record) => {
            return (
              <Flex direction={"column"} itemAlign={"start"}>
                {record.positions_by_perp?.map((item) => (
                  // <SymbolProvider symbol={item.symbol}>
                  // </SymbolProvider>
                  <FormattedText
                    value={item.mark_price}
                    type="quote"
                    key={item.symbol}
                  />
                ))}
              </Flex>
            );
          },
        },
        // Quantity
        {
          title: t("common.quantity"),
          dataIndex: "Quantity",
          // width: 202,
          render: (_: any, record) => {
            return (
              <Flex direction={"column"} itemAlign={"start"}>
                {record.positions_by_perp?.map((item) => (
                  // <SymbolProvider symbol={item.symbol}>
                  // </SymbolProvider>
                  <FormattedText
                    value={item.position_qty}
                    type="base"
                    key={item.symbol}
                  />
                ))}
              </Flex>
            );
          },
        },

        // net pnl
        // {
        //   title: t("positions.Liquidation.column.insFundTransfer"),
        //   dataIndex: "transfer_amount_to_insurance_fund",
        //   width: 202,
        //   render: (value) => {
        //     return <Text>{commifyOptional(value)}</Text>;
        //   },
        // },
        // Liquidation fee rate
        // TODO: use i18
        {
          title: (
            <TooltipButton
              tooltip={t("positions.Liquidation.col.tooltip.feeRate")}
              label={t("positions.Liquidation.column.liquidationFeeRate")}
            />
          ),
          dataIndex: "liquidationFeeRate",
          // width: 202,
          render: (value) => {
            return <Text.numeral rule="percentages">{value}</Text.numeral>;
            // return <Text>{commifyOptional(value)}</Text>;
          },
        },

        // Liquidation Fee
        {
          title: (
            <TooltipButton
              tooltip={t("positions.Liquidation.col.tooltip.fee")}
              label={t("positions.Liquidation.column.liquidationFee")}
            />
          ),
          dataIndex: "abs_liquidation_fee",
          width: 100,
          render: (abs_liquidation_fee: any, record) => {
            return (
              <Flex direction={"column"} itemAlign={"start"}>
                {record.positions_by_perp?.map((item) => (
                  // <SymbolProvider symbol={item.symbol}>
                  // </SymbolProvider>
                  <FormattedText
                    key={item.symbol}
                    value={item.abs_liquidation_fee}
                    type="quote"
                  />
                ))}
              </Flex>
            );
          },
        },
        {
          title: "",
          dataIndex: "actions",
          align: "right",
          width: 40,
          render: (_: any, record: API.Liquidation, index, ctx) => {
            const isExpanded = ctx.row.getIsExpanded();

            return (
              <button
                className="oui-p-2"
                onClick={() => {
                  ctx.row.getToggleExpandedHandler()();
                }}
              >
                <ArrowIcon className={isExpanded ? "oui-rotate-180" : ""} />
              </button>
            );
          },
        },
      ] as Column<API.Liquidation>[],
    [t],
  );

  return column;
};

const FormattedText: FC<{ value?: string | number; type: "base" | "quote" }> = (
  props,
) => {
  return <Text>{commifyOptional(props.value)}</Text>;
};

const ArrowIcon: FC<{
  className?: string;
  onClick?: () => void;
}> = (props) => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={cn(props.className, "oui-fill-base-contrast-54")}
    >
      <path
        d="M3.884 6.02a.67.67 0 0 0-.436.27.68.68 0 0 0 .187.933L7.63 9.88a.68.68 0 0 0 .749 0l3.994-2.657a.68.68 0 0 0 .187-.934.68.68 0 0 0-.936-.186L8.003 8.51l-3.62-2.407a.65.65 0 0 0-.499-.084"
        fill="currentColor"
      />
    </svg>
  );
};
