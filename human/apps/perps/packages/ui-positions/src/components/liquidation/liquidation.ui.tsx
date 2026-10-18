import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  cn,
  DataFilter,
  Flex,
  Grid,
  ListView,
  Text,
  Tooltip,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { commifyOptional } from "@paxeer-network/utils";
import { useLiquidationColumn } from "./desktop/useLiquidationColumn";
import { LiquidationState } from "./liquidation.script";
import { LiquidationCellWidget } from "./mobile";

export const Liquidation: FC<LiquidationState> = (props) => {
  const [expanded, setExpanded] = useState({});
  const column = useLiquidationColumn({});

  return (
    <Flex direction="column" width="100%" height="100%" itemAlign="start">
      {/* <Divider className="oui-w-full" /> */}
      <Flex gap={3}>
        {props.filterItems.length > 0 && (
          <DataFilter
            items={props.filterItems}
            onFilter={(value: any) => {
              props.onFilter(value);
            }}
          />
        )}
        {[1, 7, 30, 90].map((value) => {
          return (
            <button
              key={`item-${value}`}
              className="oui-relative oui-px-2 oui-py-[2px] oui-text-sm"
            >
              <div className="oui-z-10">
                <Text.gradient
                  color={props.filterDays === value ? "brand" : undefined}
                  className={
                    props.filterDays !== value
                      ? "oui-text-base-contrast-54"
                      : ""
                  }
                >
                  {`${value}D`}
                </Text.gradient>
              </div>
              <div
                className="oui-absolute oui-inset-0 oui-rounded oui-opacity-[.12] oui-gradient-primary"
                onClick={() => {
                  props.updateFilterDays(value as any);
                }}
              ></div>
            </button>
          );
        })}
      </Flex>

      <AuthGuardDataTable<API.Liquidation>
        loading={props.isLoading}
        id="oui-desktop-liquidation-content"
        columns={column}
        bordered
        dataSource={props.dataSource}
        generatedRowKey={(record: API.Liquidation) =>
          `${record.liquidation_id}`
        }
        // manualPagination={false}
        pagination={props.pagination}
        testIds={{
          body: "oui-testid-dataList-liquidation-tab-body",
        }}
        classNames={{
          root: "!oui-h-[calc(100%_-_49px)]",
        }}
        // onRow={(record, index, row) => {
        //   return {
        //     className: "[&>td:first-child]:oui-ps-0 [&>td:last-child]:oui-pe-0",
        //   };
        // }}
        expanded={expanded}
        onExpandedChange={setExpanded}
        getRowCanExpand={() => true}
        expandRowRender={(record) => {
          return <LiquidationDetails record={record.original} />;
        }}
      />
    </Flex>
  );
};

const TooltipHeaderCell: FC<{
  tooltip: string;
  label: string;
  side?: "left";
}> = (props) => {
  return (
    <th className="oui-h-10 oui-border-b oui-border-line oui-text-start oui-text-base-contrast-36">
      <Tooltip
        content={props.tooltip}
        className="oui-w-[275px] oui-bg-base-6"
        side={props.side}
        arrow={{
          className: "oui-fill-base-6",
        }}
      >
        <button className="oui-border-b oui-border-dashed oui-border-line-12">
          {props.label}
        </button>
      </Tooltip>
    </th>
  );
};

const LiquidationDetails: FC<{
  record: API.Liquidation;
}> = (props) => {
  const { margin_ratio, account_mmr, collateral_value, position_notional } =
    props.record as any;

  const { t } = useTranslation();

  return (
    <div className="oui-w-full oui-bg-base-8 oui-px-6">
      {/* <pre>{JSON.stringify(props.record, null, 2)}</pre> */}

      <table className="oui-w-full">
        <thead>
          <tr>
            <TooltipHeaderCell
              tooltip={t("positions.Liquidation.expand.tooltip.mr")}
              label={t("positions.Liquidation.expand.label.mr")}
              side="left"
            />
            <TooltipHeaderCell
              tooltip={t("positions.Liquidation.expand.tooltip.mmr")}
              label={t("positions.Liquidation.expand.label.mmr")}
            />
            <TooltipHeaderCell
              tooltip={t("positions.Liquidation.expand.tooltip.collateral")}
              label={t("positions.Liquidation.expand.label.collateral")}
            />
            <TooltipHeaderCell
              tooltip={t("positions.Liquidation.expand.tooltip.notional")}
              label={t("positions.Liquidation.expand.label.notional")}
            />
          </tr>
        </thead>
        <tbody>
          <tr>
            <td className="oui-h-10">
              <Text.numeral rule="percentages">{margin_ratio}</Text.numeral>
            </td>
            <td className="oui-h-10">
              <Text.numeral rule="percentages">{account_mmr}</Text.numeral>
            </td>
            <td className="oui-h-10">{commifyOptional(collateral_value)}</td>
            <td className="oui-h-10">{commifyOptional(position_notional)}</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
};

export const MobileLiquidation: FC<
  LiquidationState & {
    classNames?: {
      root?: string;
      content?: string;
      cell?: string;
    };
  }
> = (props) => {
  return (
    <Grid
      cols={1}
      rows={2}
      className="oui-w-full oui-grid-rows-[auto,1fr]"
      gap={1}
    >
      <Flex gap={2} p={2} className="oui-rounded-b-xl oui-bg-base-9">
        <DataFilter
          className="oui-border-none oui-py-0"
          items={props.filterItems}
          onFilter={(value: any) => {
            props.onFilter(value);
          }}
        />
      </Flex>
      <ListView
        className={cn(
          "oui-hide-scrollbar oui-w-full oui-space-y-0 oui-overflow-y-hidden",
          props.classNames?.root,
        )}
        contentClassName={cn("!oui-space-y-1", props.classNames?.content)}
        dataSource={props.dataSource}
        loadMore={props.loadMore}
        renderItem={(item, index) => (
          <LiquidationCellWidget
            item={item}
            index={index}
            classNames={{
              root: props.classNames?.cell,
            }}
          />
        )}
      />
    </Grid>
  );
};
