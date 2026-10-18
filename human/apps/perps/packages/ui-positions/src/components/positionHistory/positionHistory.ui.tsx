import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { MarginMode } from "@paxeer-network/types";
import {
  cn,
  DataFilter,
  Flex,
  Grid,
  ListView,
  Text,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { SharePnLConfig } from "@paxeer-network/ui-share";
import { SymbolProvider } from "../../provider/symbolProvider";
import { usePositionHistoryColumn } from "./desktop/usePositionHistoryColumn";
import { PositionHistoryCellWidget } from "./mobile";
import {
  PositionHistoryExt,
  PositionHistoryState,
} from "./positionHistory.script";

type PositionHistoryProps = PositionHistoryState & {
  sharePnLConfig?: SharePnLConfig;
};

// position_id may be duplicated, so include symbol, margin mode, and open timestamp.
const getPositionHistoryRowKey = (record: PositionHistoryExt) => {
  const marginMode = record.margin_mode ?? MarginMode.CROSS;

  return `${record.position_id}-${record.symbol}-${marginMode}-${record.open_timestamp}`;
};

export const PositionHistory: FC<PositionHistoryProps> = (props) => {
  const { onSymbolChange, pagination, pnlNotionalDecimalPrecision } = props;
  const column = usePositionHistoryColumn({
    onSymbolChange,
    pnlNotionalDecimalPrecision,
    sharePnLConfig: props.sharePnLConfig,
  });

  const { t } = useTranslation();

  const dayLabel: Record<number, string> = useMemo(() => {
    return {
      1: t("common.select.1d"),
      7: t("common.select.7d"),
      30: t("common.select.30d"),
      90: t("common.select.90d"),
    };
  }, [t]);

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
                  className={cn(
                    "oui-whitespace-nowrap oui-break-normal",
                    props.filterDays !== value
                      ? "oui-text-base-contrast-54"
                      : "",
                  )}
                >
                  {dayLabel[value] || `${value}D`}
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

      <AuthGuardDataTable<PositionHistoryExt>
        loading={props.isLoading}
        id="oui-desktop-positions-content"
        columns={column}
        bordered
        dataSource={props.dataSource}
        generatedRowKey={getPositionHistoryRowKey}
        renderRowContainer={(record: any, index: number, children: any) => (
          <SymbolProvider symbol={record.symbol}>{children}</SymbolProvider>
        )}
        manualPagination={false}
        pagination={pagination}
        manualSorting={true}
        onSort={props.onSort}
        initialSort={
          props.initialSort
            ? {
                sortKey: props.initialSort.sortKey,
                sort: props.initialSort.sortOrder,
              }
            : undefined
        }
        testIds={{
          body: "oui-testid-dataList-positionHistory-tab-body",
        }}
        classNames={{
          root: "!oui-h-[calc(100%_-_49px)]",
        }}
      />
    </Flex>
  );
};

export const MobilePositionHistory: FC<
  PositionHistoryState & {
    classNames?: { root?: string; content?: string; cell?: string };
    sharePnLConfig?: SharePnLConfig;
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
        keyExtractor={getPositionHistoryRowKey}
        renderItem={(item, index) => (
          <SymbolProvider symbol={item.symbol}>
            <PositionHistoryCellWidget
              item={item}
              index={index}
              onSymbolChange={props.onSymbolChange}
              classNames={{
                root: props.classNames?.cell,
              }}
              sharePnLConfig={props.sharePnLConfig}
            />
          </SymbolProvider>
        )}
      />
    </Grid>
  );
};
