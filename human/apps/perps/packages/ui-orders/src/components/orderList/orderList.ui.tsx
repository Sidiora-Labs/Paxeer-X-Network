import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import type { API } from "@paxeer-network/types";
import {
  Flex,
  ListView,
  Button,
  Grid,
  Picker,
  DataFilter,
  cn,
  TableFeatures,
} from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { formatSymbol } from "@paxeer-network/utils";
import { grayCell } from "../../utils/util";
import { TabType } from "../orders.widget";
import { SymbolProvider } from "../provider/symbolProvider";
import { useOrderColumn } from "./desktop/useColumn";
import { OrderCellWidget } from "./mobile";
import { OrdersBuilderState } from "./orderList.script";
import { OrderListProvider } from "./orderListProvider";
import { TPSLOrderRowProvider } from "./tpslOrderRowContext";

const getOrderRowKey = <T extends API.Order | API.AlgoOrder>(item: T) =>
  item.algo_order_id !== undefined
    ? `algo-${item.algo_order_id}`
    : `order-${(item as API.Order).order_id}`;

export const DesktopOrderList: FC<
  OrdersBuilderState & { testIds?: { tableBody?: string } }
> = (props) => {
  const { sharePnLConfig, ...rest } = props;
  const columns = useOrderColumn({
    _type: props.type,
    onSymbolChange: props.onSymbolChange,
    pnlNotionalDecimalPrecision: props.pnlNotionalDecimalPrecision,
    sharePnLConfig,
    symbolsInfo: props.symbolsInfo,
  });

  return (
    <OrderListProvider
      cancelOrder={props.cancelOrder}
      editOrder={props.updateOrder}
      cancelAlgoOrder={props.cancelAlgoOrder}
      editAlgoOrder={props.updateAlgoOrder}
      // cancelTPSLOrder={props.cancelTPSLOrder}
    >
      <Flex direction="column" width="100%" height="100%" itemAlign="start">
        {props.filterItems.length > 0 && (
          <DataFilter
            items={props.filterItems}
            onFilter={(value: any) => {
              props.onFilter(value);
            }}
            trailing={
              [TabType.pending, TabType.tp_sl].includes(props.type) && (
                <CancelAll {...props} />
              )
            }
          />
        )}
        <AuthGuardDataTable
          columns={columns}
          loading={props.isLoading}
          dataSource={props.dataSource}
          bordered
          ignoreLoadingCheck={true}
          testIds={{
            body: props.testIds?.tableBody,
          }}
          classNames={{
            header: "oui-h-[38px]",
            root: "oui-items-start !oui-h-[calc(100%_-_49px)]",
          }}
          onRow={(record, index) => {
            return {
              className: cn(
                "oui-h-[48px]",
                grayCell(record)
                  ? "oui-text-base-contrast-20"
                  : "oui-text-base-contrast-80",
              ),
            };
          }}
          generatedRowKey={getOrderRowKey}
          renderRowContainer={(record: any, index, children) => {
            if (
              props.type === TabType.tp_sl ||
              props.type === TabType.pending
            ) {
              children = (
                <TPSLOrderRowProvider order={record}>
                  {children}
                </TPSLOrderRowProvider>
              );
            }

            return (
              <SymbolProvider symbol={record.symbol}>{children}</SymbolProvider>
            );
          }}
          pagination={props.pagination}
          manualPagination={props.manualPagination}
          features={[TableFeatures.DownloadFeature]}
          getTableInstance={(table) => {
            props.tableInstance.current = table;
          }}
        />
      </Flex>
    </OrderListProvider>
  );
};

export const MobileOrderList: FC<
  OrdersBuilderState & {
    classNames?: { root?: string; cell?: string; content?: string };
    showFilter?: boolean;
  }
> = (props) => {
  const { t } = useTranslation();

  return (
    <OrderListProvider
      cancelOrder={props.cancelOrder}
      editOrder={props.updateOrder}
      cancelAlgoOrder={props.cancelAlgoOrder}
      editAlgoOrder={props.updateAlgoOrder}
      // cancelTPSLOrder={props.cancelTPSLOrder}
    >
      <Grid
        cols={1}
        rows={2}
        className="oui-w-full oui-grid-rows-[auto,1fr]"
        gap={2}
      >
        {/* <Filter
          items={props.filterItems}
          onFilter={(value: any) => {
            props.onFilter(value);
          }}
          className="oui-px-3"
          
        /> */}

        {props.showFilter ? (
          <Flex gap={2} p={2} className="oui-rounded-b-xl oui-bg-base-9">
            {props.filterItems.map((item) => {
              // not support range type
              if (item.type !== "select") {
                return null;
              }
              return (
                <Picker
                  key={`item-${item.name}`}
                  options={item.options}
                  size={"sm"}
                  value={item.value}
                  className="oui-text-2xs oui-text-base-contrast-54 "
                  placeholder={
                    item.name === "side"
                      ? t("common.side.all")
                      : item.name === "status"
                        ? t("common.status.all")
                        : ""
                  }
                  onValueChange={(value) => {
                    //
                    props.onFilter?.({ name: item.name, value: value });
                  }}
                />
              );
            })}
          </Flex>
        ) : (
          <div></div>
        )}
        <ListView
          className={props.classNames?.root}
          contentClassName={props.classNames?.content}
          dataSource={props.dataSource}
          keyExtractor={getOrderRowKey}
          loadMore={props.loadMore}
          isLoading={props.isLoading}
          renderItem={(item, index) => {
            let children = (
              <OrderCellWidget
                item={item}
                index={index}
                className={props.classNames?.cell}
                type={props.type}
                onSymbolChange={props.onSymbolChange}
                sharePnLConfig={props.sharePnLConfig}
              />
            );
            if ([TabType.tp_sl, TabType.pending].includes(props.type)) {
              children = (
                <TPSLOrderRowProvider order={item}>
                  {children}
                </TPSLOrderRowProvider>
              );
            }
            return (
              <SymbolProvider symbol={item.symbol}>{children}</SymbolProvider>
            );
          }}
        />
      </Grid>
    </OrderListProvider>
  );
};

const CancelAll: FC<OrdersBuilderState> = (props) => {
  const { t } = useTranslation();
  const { symbol } = props;

  const formattedSymbol = symbol ? formatSymbol(symbol, "base") : symbol;

  return (
    <Button
      variant="outlined"
      color="secondary"
      size="xs"
      disabled={(props.dataSource?.length ?? 0) == 0}
      className="disabled:oui-bg-transport"
      onClick={(e) => props.onCancelAll()}
      data-testid={`oui-testid-dataList-${props.type.toLowerCase()}-cancelAll-button`}
    >
      {symbol
        ? t("orders.cancelAll.ofSymbol", { symbol: formattedSymbol })
        : t("orders.cancelAll")}
    </Button>
  );
};
