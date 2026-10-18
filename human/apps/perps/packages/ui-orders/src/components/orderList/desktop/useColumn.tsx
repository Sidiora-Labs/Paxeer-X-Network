import { useMemo } from "react";
import { format } from "date-fns";
import { SymbolsInfo, utils } from "@paxeer-network/hooks";
import { useTranslation, i18n } from "@paxeer-network/i18n";
import {
  AlgoOrderRootType,
  AlgoOrderType,
  API,
  OrderSide,
  OrderStatus,
  OrderType,
} from "@paxeer-network/types";
import { cn, Column, Flex, Text } from "@paxeer-network/ui";
import { Badge } from "@paxeer-network/ui";
import { SharePnLConfig, SharePnLDialogId } from "@paxeer-network/ui-share";
import {
  isPositionalTPSL,
  isEntirePositionTPSL,
  getTPSLQuantity,
} from "@paxeer-network/utils";
import {
  commifyOptional,
  Decimal,
  formatNum,
  getTrailingStopPrice,
} from "@paxeer-network/utils";
import { getPositionalQuantityText } from "../../../utils/util";
import {
  grayCell,
  parseBadgesFor,
  upperCaseFirstLetter,
  isTrailingStopOrder,
  getNotional,
} from "../../../utils/util";
import { TabType } from "../../orders.widget";
import { useSymbolContext } from "../../provider/symbolContext";
import { ShareButtonWidget } from "../../shareButton";
import { AvgPrice } from "./avgPrice";
import { BracketOrderPrice } from "./bracketOrderPrice";
import { CancelButton } from "./cancelBtn";
import { ActivedPriceCell } from "./components/activedPriceCell";
import { PriceCell } from "./components/priceCell";
import { QuantityCell } from "./components/quantityCell";
import { TrailingCallbackCell } from "./components/trailingCallbackCell";
import { TriggerPriceCell } from "./components/triggerPriceCell";
import { OrderSymbolCell } from "./orderSymbolCell";
import { Renew } from "./renew";
import { TP_SLEditButton } from "./tpslEdit";
import { TPSLOrderPrice, getTPSLOrderPrice } from "./tpslPrice";
import { OrderTriggerPrice } from "./tpslTriggerPrice";

export const useOrderColumn = (props: {
  _type: TabType;
  onSymbolChange?: (symbol: API.Symbol) => void;
  pnlNotionalDecimalPrecision?: number;
  sharePnLConfig?: SharePnLConfig;
  symbolsInfo?: SymbolsInfo;
}) => {
  const {
    _type,
    onSymbolChange,
    pnlNotionalDecimalPrecision,
    sharePnLConfig,
    symbolsInfo,
  } = props;
  const { t } = useTranslation();

  const columns = useMemo(() => {
    switch (_type) {
      case TabType.all:
        return [
          instrument({
            width: 200,
            showType: true,
            onSymbolChange: onSymbolChange,
            enableSort: false,
          }),
          side({ width: 130 }),
          fillAndQuantity({
            width: 130,
            disableEdit: true,
            className: "oui-ps-0 oui-pe-0",
            enableSort: false,
          }),
          price({
            width: 130,
            title: t("common.orderPrice"),
            disableEdit: true,
            enableSort: false,
          }),
          avgOpen({ width: 130, enableSort: false, symbolsInfo }),
          tpslTriggerPrice({ width: 130, symbolsInfo: props.symbolsInfo }),
          realizedPnL({
            width: 124,
            pnlNotionalDecimalPrecision: pnlNotionalDecimalPrecision,
            sharePnLConfig: sharePnLConfig,
            symbolsInfo: props.symbolsInfo,
            hideShare: true,
          }),
          estTotal({ width: 130, enableSort: false }),
          fee({ width: 130 }),
          status({ width: 130 }),
          reduceOnly({ width: 130 }),
          hidden({ width: 130 }),
          cancelBtn({ width: 130 }),
          orderTime({ width: 160, enableSort: false }),
        ];
      case TabType.pending:
        return [
          instrument({
            width: 200,
            showType: true,
            onSymbolChange: onSymbolChange,
            enableSort: false,
          }),
          // side({ width: 162 }),
          fillAndQuantity({
            width: 162,
            className: "oui-pe-0",
            enableSort: false,
          }),
          price({ width: 162, className: "oui-pe-0", enableSort: false }),
          trailingCallback({ width: 162 }),
          triggerPrice({ width: 162, className: "oui-pe-0", isPending: true }),
          bracketOrderPrice({ width: 130 }),
          estTotal({ width: 162, isPending: true }),
          reduceOnly({ width: 162 }),
          hidden({ width: 162 }),
          orderTime({ width: 162, enableSort: false }),
          pendingTabCancelBtn({ width: 80 }),
        ];
      case TabType.tp_sl:
        return [
          instrument({
            width: 200,
            showType: true,
            onSymbolChange: onSymbolChange,
            enableSort: false,
          }),
          // side({ width: 176 }),
          quantity({ width: 176 }),
          tpslTriggerPrice({ width: 176, symbolsInfo }),
          tpslPrice({ width: 176, disableEdit: true }),
          tpslNotional({ width: 176 }),
          reduceOnly({ width: 176 }),
          orderTime({ width: 176, enableSort: false }),
          tpslAction({ width: 176 }),
        ];
      case TabType.filled:
        return [
          instrument({
            showType: true,
            width: 200,
            onSymbolChange: onSymbolChange,
          }),
          // type({ width: 124 }),
          // side({ width: 124 }),
          fillAndQuantity({
            width: 124,
            disableEdit: true,
            className: "oui-ps-0 oui-pe-0",
          }),
          price({
            width: 124,
            title: t("common.orderPrice"),
            disableEdit: true,
          }),
          avgPrice({ width: 124 }),
          triggerPrice({ width: 124, disableEdit: true }),
          realizedPnL({
            width: 124,
            pnlNotionalDecimalPrecision: pnlNotionalDecimalPrecision,
            sharePnLConfig: sharePnLConfig,
            symbolsInfo,
            hideShare: true,
          }),
          estTotal({ width: 124 }),
          fee({ width: 124 }),
          status({ width: 124 }),
          reduceOnly({ width: 124 }),
          hidden({ width: 124 }),
          orderTime({ width: 176 }),
        ];
      case TabType.cancelled:
        return [
          instrument({
            showType: true,
            width: 200,
            onSymbolChange: onSymbolChange,
            enableSort: false,
          }),
          // side({ width: 124 }),
          fillAndQuantity({
            width: 124,
            disableEdit: true,
            className: "oui-ps-0 oui-pe-0",
            enableSort: false,
          }),
          price({ width: 124, disableEdit: true, enableSort: false }),
          avgOpen({ width: 124, enableSort: false }),
          triggerPrice({ width: 124, disableEdit: true }),
          estTotal({ width: 124 }),
          fee({ width: 124 }),
          status({ width: 124 }),
          reduceOnly({ width: 124 }),
          hidden({ width: 124 }),
        ];
      case TabType.rejected:
        return [
          instrument({
            showType: true,
            width: 200,
            onSymbolChange: onSymbolChange,
          }),
          // side({ width: 124 }),
          fillAndQuantity({
            width: 124,
            disableEdit: true,
            className: "oui-ps-0 oui-pe-0",
          }),
          price({ width: 124, disableEdit: true }),
          avgOpen({ width: 124 }),
          triggerPrice({ width: 124, disableEdit: true }),
          estTotal({ width: 124 }),
          fee({ width: 124 }),
          status({ width: 124 }),
          reduceOnly({ width: 124 }),
          hidden({ width: 124 }),
          orderTime({ width: 176 }),
        ];
      case TabType.orderHistory:
        return [
          instrument({
            showType: true,
            width: 200,
            onSymbolChange: onSymbolChange,
          }),
          // side({ width: 124 }),
          fillAndQuantity({
            width: 150,
            disableEdit: true,
            className: "oui-ps-6 oui-pe-0",
          }),
          price({
            width: 124,
            disableEdit: true,
            tabType: TabType.orderHistory,
          }),
          avgOpen({ width: 124 }),
          triggerPrice({ width: 124, disableEdit: true }),
          realizedPnL({
            width: 124,
            pnlNotionalDecimalPrecision: pnlNotionalDecimalPrecision,
            sharePnLConfig: sharePnLConfig,
            symbolsInfo: props.symbolsInfo,
            hideShare: true,
          }),
          estTotal({ width: 124 }),
          trailingCallback({ width: 124, disableEdit: true }),
          fee({ width: 124 }),
          status({ width: 124 }),
          reduceOnly({ width: 124 }),
          hidden({ width: 124 }),
          orderTime({ width: 150 }),
          cancelBtn({ width: 80 }),
        ];
    }
  }, [_type, pnlNotionalDecimalPrecision, sharePnLConfig, t]);

  return columns as Column[];

  // return columns();
};

function instrument(option?: {
  showType?: boolean;
  enableSort?: boolean;
  width?: number;
  onSymbolChange?: (symbol: API.Symbol) => void;
}): Column<API.Order> {
  return {
    title: i18n.t("common.symbol"),
    dataIndex: "symbol",
    fixed: "start",
    // className: "oui-h-[48px]",
    width: option?.width,
    onSort: option?.enableSort
      ? (r1, r2) => {
          return r1.symbol.localeCompare(r2.symbol);
          // if (sortOrder === "asc") {
          //   return r1.symbol.localeCompare(r2.symbol);
          // }
          // return r2.symbol.localeCompare(r1.symbol);
        }
      : undefined,
    renderPlantText: (value: string, record) => {
      const badges = parseBadgesFor(record)?.join(",");
      const displayBadges = badges?.length ? ` (${badges})` : "";
      return `${value.split("_")[1]}-PERP${displayBadges}`;
    },
    render: (value: string, record) => {
      const showGray = grayCell(record);

      return (
        <Flex gap={2} className="oui-py-1">
          <div
            className={cn(
              "oui-h-[34px] oui-w-1 oui-shrink-0 oui-rounded-[1px]",
              record.side === OrderSide.BUY
                ? "oui-bg-trade-profit"
                : "oui-bg-trade-loss",
            )}
          />
          <Flex direction="column" itemAlign={"start"} gap={1}>
            <OrderSymbolCell
              symbol={value}
              onSymbolChange={option?.onSymbolChange}
            />

            {option?.showType && (
              <Flex direction={"row"} gap={1} wrap="wrap">
                {parseBadgesFor(record)?.map((e, index) => (
                  <Badge
                    key={index}
                    color={
                      e.toLocaleLowerCase() === "position"
                        ? showGray
                          ? "neutral"
                          : "primary"
                        : "neutral"
                    }
                    size="xs"
                    className="oui-whitespace-nowrap oui-break-normal"
                  >
                    {e}
                  </Badge>
                ))}
              </Flex>
            )}
          </Flex>
        </Flex>
      );
    },
  };
}

function side(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("common.side"),
    dataIndex: "side",
    width: option?.width,
    // className: "oui-h-[48px]",
    onSort: option?.enableSort
      ? (r1, r2, sortOrder) => {
          return r2.side.localeCompare(r1.side);
          // if (sortOrder === "asc") {
          //   return r2.side.localeCompare(r1.side);
          // }
          // return r1.side.localeCompare(r2.side);
        }
      : undefined,
    renderPlantText: (value: string, record) => upperCaseFirstLetter(value),
    render: (value: string, record) => {
      const clsName = grayCell(record)
        ? "oui-text-base-contrast-20"
        : value === OrderSide.BUY
          ? "oui-text-trade-profit"
          : "oui-text-trade-loss";
      return (
        <span className={cn("oui-font-semibold", clsName)}>
          {upperCaseFirstLetter(value)}
        </span>
      );
    },
  };
}

function type(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("common.type"),
    dataIndex: "type",
    width: option?.width,
    className: option?.className,
    formatter: (value: string, record: any) => {
      if (!!record.parent_algo_type) {
        if (record.algo_type === AlgoOrderType.STOP_LOSS) {
          return isPositionalTPSL(record) ? `Position SL` : "SL";
        }

        if (record.algo_type === AlgoOrderType.TAKE_PROFIT) {
          return isPositionalTPSL(record) ? `Position TP` : "TP";
        }
      }

      if (record.algo_order_id) {
        return `Stop ` + `${record.type}`.toLowerCase();
      }
      return upperCaseFirstLetter(value);
    },
  };
}

function fillAndQuantity(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  disableEdit?: boolean;
}): Column<API.AlgoOrderExt> {
  return {
    title: i18n.t("orders.column.fill&Quantity"),
    dataIndex: "fill_quantity",
    className: option?.className,
    width: option?.width,
    onSort:
      (option?.enableSort ?? false)
        ? (a, b) => {
            const aQuantity =
              (a.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
                ? 0
                : a.quantity) ?? 0;
            const bQuantity =
              (b.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
                ? 0
                : b.quantity) ?? 0;

            return compareNumbers(aQuantity, bQuantity);

            // if (type == "asc") {
            //   return compareNumbers(aQuantity, bQuantity);
            // }
            // return compareNumbers(bQuantity, aQuantity);
          }
        : undefined,

    renderPlantText: (value: string, record: any) => {
      if (isEntirePositionTPSL(record)) {
        return i18n.t("tpsl.entirePosition");
      }

      const executed = (record as API.OrderExt).total_executed_quantity;
      const first =
        "algo_type" in record && record.algo_type === AlgoOrderRootType.TP_SL
          ? ""
          : `${executed} / `;
      return (
        first +
        `${isPositionalTPSL(record) ? (getTPSLQuantity(record) ?? "--") : record.quantity}`
      );
    },
    render: (value: string, record: any) => {
      if (isEntirePositionTPSL(record)) {
        return i18n.t("tpsl.entirePosition");
      }
      return (
        <QuantityCell
          order={record}
          disabled={option?.disableEdit || isPositionalTPSL(record)}
        />
      );
      // return value;
    },
  };
}

function quantity(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.AlgoOrderExt> {
  return {
    title: i18n.t("common.quantity"),
    className: option?.className,
    dataIndex: "quantity",
    width: option?.width,
    onSort:
      (option?.enableSort ?? false)
        ? (a, b) => {
            const aQuantity =
              (a.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
                ? 0
                : a.quantity) ?? 0;
            const bQuantity =
              (b.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
                ? 0
                : b.quantity) ?? 0;
            return compareNumbers(aQuantity, bQuantity);
            // if (type == "asc") {
            //   return compareNumbers(aQuantity, bQuantity);
            // }
            // return compareNumbers(bQuantity, aQuantity);
          }
        : undefined,
    renderPlantText: (value: string, record: any) => {
      if (isPositionalTPSL(record)) return getPositionalQuantityText(record);
      if (isEntirePositionTPSL(record)) {
        return i18n.t("tpsl.entirePosition");
      }

      const executed = (record as API.OrderExt).total_executed_quantity;
      const first =
        "algo_type" in record && record.algo_type === AlgoOrderRootType.TP_SL
          ? ""
          : `${executed}/`;
      return (
        first +
        `${isPositionalTPSL(record) ? (getTPSLQuantity(record) ?? "--") : record.quantity}`
      );
    },
    render: (value: string, record: any) => {
      if (isPositionalTPSL(record) && record.child_orders?.length)
        return getPositionalQuantityText(record);
      if (isEntirePositionTPSL(record)) {
        return i18n.t("tpsl.entirePosition");
      }
      return (
        <QuantityCell order={record} disabled={isPositionalTPSL(record)} />
      );
      // return value;
    },
  };
}

function price(option?: {
  title?: string;
  enableSort?: boolean;
  width?: number;
  className?: string;
  disableEdit?: boolean;
  tabType?: TabType;
}): Column<API.Order> {
  return {
    title: option?.title ?? i18n.t("common.price"),
    dataIndex: "price",
    className: option?.className,
    width: option?.width,
    onSort:
      (option?.enableSort ?? false)
        ? (a, b, type) => {
            return compareNumbers(a.price ?? 0, b.price ?? 0);
          }
        : undefined,
    renderPlantText: (value: string, record: any) => {
      const isTrailingStop = isTrailingStopOrder(record);
      if (isTrailingStop) {
        // TODO: fix precision
        return option?.tabType === TabType.orderHistory
          ? i18n.t("common.marketPrice")
          : getTrailingStopPrice(record) || "--";
      }
      return commifyOptional(record.price, {
        fallback: i18n.t("common.marketPrice"),
      });
    },
    render: (value: string, record: any) => {
      const isTrailingStop = isTrailingStopOrder(record);
      if (isTrailingStop) {
        // TODO: fix precision
        return option?.tabType === TabType.orderHistory
          ? i18n.t("common.marketPrice")
          : getTrailingStopPrice(record) || "--";
      }

      return <PriceCell order={record} disabled={option?.disableEdit} />;
    },
  };
}

function tpslPrice(option?: {
  title?: string;
  enableSort?: boolean;
  width?: number;
  className?: string;
  disableEdit?: boolean;
}): Column<API.Order> {
  return {
    title: option?.title ?? i18n.t("common.price"),
    dataIndex: "price",
    className: option?.className,
    width: option?.width,
    onSort: option?.enableSort,
    renderPlantText: (value: string, record: any) => {
      const { tpTriggerPrice, slTriggerPrice } = getTPSLOrderPrice(record);
      const callback = `${tpTriggerPrice || ""}${
        slTriggerPrice ? `${tpTriggerPrice ? "\n" : ""}${slTriggerPrice}` : ""
      }`;

      return callback.length > 0 ? callback : "--";
    },
    render: (value: string, record: any) => {
      return <TPSLOrderPrice />;
    },
  };
}

function avgPrice(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  disableEdit?: boolean;
  symbolsInfo?: SymbolsInfo;
}): Column<API.Order> {
  return {
    title: i18n.t("common.avgPrice"),
    dataIndex: "average_executed_price",
    className: option?.className,
    width: option?.width,
    onSort: option?.enableSort,
    renderPlantText: (value: string, record: any) => {
      return commifyOptional(value);
    },
    render: (value: string, record: any) => {
      return <AvgPrice symbol={record.symbol} value={value} />;
    },
  };
}

function triggerPrice(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  disableEdit?: boolean;
  isPending?: boolean;
}): Column<API.Order> {
  const { isPending } = option || {};
  const checkOrderType = (record: any) => {
    const isAlgoOrder = record?.algo_order_id !== undefined;
    const isBracketOrder = record?.algo_type === "BRACKET";
    return isAlgoOrder && !isBracketOrder;
  };

  return {
    title: i18n.t("common.trigger"),
    className: option?.className,
    dataIndex: "trigger_price",
    width: option?.width,
    onSort: option?.enableSort,
    renderPlantText: (value: string, record: any) => {
      if (!checkOrderType(record)) {
        return "--";
      }
      if (isTrailingStopOrder(record) && isPending) {
        return record.activated_price
          ? commifyOptional(record.activated_price)
          : "--";
      }
      return commifyOptional(value);
    },
    render: (value: string, record: any) => {
      if (!checkOrderType(record)) {
        return "--";
      }

      if (isTrailingStopOrder(record) && isPending) {
        return (
          <ActivedPriceCell order={record} disabled={option?.disableEdit} />
        );
      }
      return <TriggerPriceCell order={record} disabled={option?.disableEdit} />;
    },
  };
}

function tpslTriggerPrice(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  title?: string;
  symbolsInfo?: SymbolsInfo;
}): Column<API.Order> {
  return {
    title: option?.title ?? i18n.t("common.trigger"),
    className: option?.className,
    dataIndex: "tpsl_trigger_price",
    width: option?.width,
    onSort: option?.enableSort,
    renderPlantText: (value: string, record: any) => {
      const info = option?.symbolsInfo?.[record.symbol];
      const quote_dp = info?.("quote_dp");
      // @ts-ignore
      const { sl_trigger_price, tp_trigger_price } =
        !("algo_type" in record) || !Array.isArray(record.child_orders)
          ? {}
          : utils.findTPSLFromOrder(record);

      const callback =
        (tp_trigger_price != null
          ? `${i18n.t("tpsl.tp")}: ${commifyOptional(tp_trigger_price, {
              fix: quote_dp,
              padEnd: true,
            })}`
          : "") +
        (sl_trigger_price != null
          ? `${tp_trigger_price ? "\n" : ""}${i18n.t(
              "tpsl.sl",
            )}: ${commifyOptional(sl_trigger_price, {
              fix: quote_dp,
              padEnd: true,
            })}`
          : "");
      return callback.length > 0 ? callback : "--";
    },
    render: (value: string, record: any) => <OrderTriggerPrice />,
  };
}

function bracketOrderPrice(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}) {
  return {
    title: i18n.t("common.tpsl"),
    className: option?.className,
    dataIndex: "bracketOrderPrice",
    width: option?.width,
    onSort: option?.enableSort,
    renderPlantText: (value: string, record: any) => {
      const getTPSLTriggerPrice = (): {
        sl_trigger_price?: number;
        tp_trigger_price?: number;
      } => {
        if (!("algo_type" in record) || !Array.isArray(record.child_orders)) {
          return {};
        }
        return utils.findTPSLFromOrder(record.child_orders[0]);
      };

      const { sl_trigger_price, tp_trigger_price } = getTPSLTriggerPrice();

      const callback =
        (tp_trigger_price != null
          ? `${i18n.t("tpsl.tp")}: ${tp_trigger_price}`
          : "") +
        (sl_trigger_price != null
          ? `${tp_trigger_price ? "\n" : ""}${i18n.t(
              "tpsl.sl",
            )}: ${sl_trigger_price}`
          : "");
      return callback.length > 0 ? callback : "--";
    },
    render: (value: string, record: any) => (
      <BracketOrderPrice order={record} />
    ),
  };
}

function estTotal(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  isPending?: boolean;
}): Column<API.Order> {
  return {
    title: i18n.t("common.notional"),
    dataIndex: "executed",
    width: option?.width,
    className: option?.className,
    onSort:
      (option?.enableSort ?? false)
        ? (a, b, type) => {
            const aTotal = isEntirePositionTPSL(a)
              ? 0
              : a.total_executed_quantity === 0 ||
                  Number.isNaN(a.average_executed_price) ||
                  a.average_executed_price === null
                ? 0
                : a.total_executed_quantity * a.average_executed_price;
            const bTotal = isEntirePositionTPSL(b)
              ? 0
              : b.total_executed_quantity === 0 ||
                  Number.isNaN(b.average_executed_price) ||
                  b.average_executed_price === null
                ? 0
                : b.total_executed_quantity * b.average_executed_price;
            return compareNumbers(aTotal, bTotal);
            // if (type === "asc") {
            //   return compareNumbers(aTotal, bTotal);
            // }
            // return compareNumbers(bTotal, aTotal);
          }
        : undefined,
    renderPlantText: (value: string, record: any) => {
      const estTotal = estTotalValue(record, option?.isPending ?? false);

      if (estTotal === "Entire position") {
        return i18n.t("tpsl.entirePosition");
      }

      return commifyOptional(
        estTotalValue(record, option?.isPending ?? false),
        { fix: 2 },
      );
    },
    render: (value: string, record: any) => {
      const estTotal = estTotalValue(record, option?.isPending ?? false);

      if (estTotal === "Entire position") {
        return i18n.t("tpsl.entirePosition");
      }

      return (
        <Text.numeral rm={Decimal.ROUND_DOWN} dp={2}>
          {estTotal}
        </Text.numeral>
      );
    },
  };
}

function realizedPnL(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  pnlNotionalDecimalPrecision?: number;
  sharePnLConfig?: SharePnLConfig;
  symbolsInfo?: SymbolsInfo;
  hideShare?: boolean;
}): Column<API.Order> {
  return {
    title: i18n.t("common.realizedPnl"),
    dataIndex: "realized_pnl",
    width: option?.width,
    className: option?.className,
    renderPlantText: (_value: string, record: any) => {
      const info = option?.symbolsInfo?.[record.symbol];
      const quote_dp = info?.("quote_dp");
      const dp = option?.pnlNotionalDecimalPrecision ?? quote_dp;
      const value = new Decimal(_value ?? 0)
        .toDecimalPlaces(dp, Decimal.ROUND_DOWN)
        .toNumber();

      const formatValue = commifyOptional(value);

      return value > 0 ? `+${formatValue}` : formatValue;
    },
    render: (_value: number | undefined, record: any) => {
      // TODO(hooks): Extract this render body into a cell component before using hooks.
      const { quote_dp } = useSymbolContext();
      const dp = option?.pnlNotionalDecimalPrecision ?? quote_dp;
      const value = new Decimal(_value ?? 0).toNumber();
      // wraper flex
      return (
        <Flex gap={1}>
          <Text.pnl
            dp={dp}
            padding={false}
            intensity={(value ?? 0) == 0 ? 80 : undefined}
            showIdentifier={(value ?? 0) > 0}
            coloring={(value ?? 0) != 0}
          >
            {value ?? "--"}
          </Text.pnl>
          {!option?.hideShare && (
            <ShareButtonWidget
              order={record}
              sharePnLConfig={option?.sharePnLConfig}
              modalId={SharePnLDialogId}
            />
          )}
        </Flex>
      );
    },
  };
}

function reduceOnly(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("orderEntry.reduceOnly"),
    dataIndex: "reduce_only",
    width: option?.width,
    className: option?.className,
    renderPlantText: (value: string, record: any) => {
      return value ? i18n.t("common.yes") : i18n.t("common.no");
    },
    render: (value: boolean) => {
      return <Text>{value ? i18n.t("common.yes") : i18n.t("common.no")}</Text>;
    },
  };
}

function hidden(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("orders.column.hidden"),
    dataIndex: "visible",
    width: option?.width,
    className: option?.className,
    renderPlantText: (value: number, record: any) => {
      return value !== 0 ? i18n.t("common.no") : i18n.t("common.yes");
    },
    render: (value: number, record) => {
      return (
        <Text>
          {record.visible_quantity !== 0
            ? i18n.t("common.no")
            : i18n.t("common.yes")}
        </Text>
      );
    },
  };
}

function orderTime(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  formatString?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("orders.column.orderTime"),
    dataIndex: "created_time",
    width: option?.width,
    onSort: option?.enableSort,
    className: option?.className,
    renderPlantText: (value: string, record: any) => {
      const date = new Date(value);
      const formattedDate = format(date, "yyyy-MM-dd HH:mm:ss");
      return formattedDate;
    },
    render: (value: string) => (
      <Text.formatted
        rule={"date"}
        formatString={option?.formatString || "yyyy-MM-dd HH:mm:ss"}
        className="oui-break-normal oui-whitespace-nowrap oui-font-semibold"
      >
        {value}
      </Text.formatted>
    ),
  };
}

function fee(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("common.fee"),
    dataIndex: "total_fee",
    width: option?.width,
    onSort: option?.enableSort,
    className: option?.className,
  };
}

function notional(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("common.notional"),
    dataIndex: "notional",
    width: option?.width,
    onSort: option?.enableSort,
    className: option?.className,
    renderPlantText: (value: number, record: any) => {
      return commifyOptional(value, { fix: 2 });
    },
    render: (value?: string) => (
      <Text.notional className="oui-break-normal oui-whitespace-nowrap oui-font-semibold">
        {value ?? "--"}
      </Text.notional>
    ),
  };
}

function tpslNotional(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: i18n.t("common.notional"),
    dataIndex: "executed",
    width: option?.width,
    onSort: option?.enableSort,
    className: option?.className,
    renderPlantText: (value: any, record: any) => {
      if (isEntirePositionTPSL(record)) {
        return i18n.t("tpsl.entirePosition");
      }
      return commifyOptional(
        record.quantity === 0
          ? "--"
          : `${formatNum
              .notional(new Decimal(record.mark_price).mul(record.quantity))
              ?.toNumber()}`,
      );
    },
    render: (value: any, record: any) => {
      if (isEntirePositionTPSL(record)) {
        return i18n.t("tpsl.entirePosition");
      }

      return (
        <Text.numeral className="oui-break-normal oui-whitespace-nowrap oui-font-semibold">
          {record.quantity === 0
            ? "--"
            : `${formatNum
                .notional(new Decimal(record.mark_price).mul(record.quantity))
                ?.toNumber()}`}
        </Text.numeral>
      );
    },
  };
}

function status(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
}): Column<API.Order> {
  const statusMap = {
    [OrderStatus.NEW]: i18n.t("orders.status.pending"),
    [OrderStatus.FILLED]: i18n.t("orders.status.filled"),
    [OrderStatus.PARTIAL_FILLED]: i18n.t("orders.status.partialFilled"),
    [OrderStatus.CANCELLED]: i18n.t("orders.status.canceled"),
    [OrderStatus.REJECTED]: i18n.t("orders.status.rejected"),
    [OrderStatus.INCOMPLETE]: i18n.t("orders.status.incomplete"),
    [OrderStatus.COMPLETED]: i18n.t("orders.status.completed"),
  };
  return {
    title: i18n.t("common.status"),
    dataIndex: "status",
    width: option?.width,
    onSort: option?.enableSort,
    className: option?.className,
    renderPlantText: (value: string, record: any) => {
      const status = value || record.algo_status;
      return (
        statusMap[status as keyof typeof statusMap] ||
        upperCaseFirstLetter(status)
      );
    },
    render: (value: string, record: any) => {
      const status = value || record.algo_status;
      return (
        statusMap[status as keyof typeof statusMap] ||
        upperCaseFirstLetter(status)
      );
    },
  };
}

function avgOpen(option?: {
  enableSort?: boolean;
  width?: number;
  className?: string;
  symbolsInfo?: SymbolsInfo;
}): Column<API.Order> {
  return {
    title: i18n.t("common.avgPrice"),
    dataIndex: "average_executed_price",
    width: option?.width,
    onSort:
      (option?.enableSort ?? false)
        ? (a, b) => {
            return compareNumbers(
              a.average_executed_price ?? 0,
              b.average_executed_price ?? 0,
            );
          }
        : undefined,
    className: option?.className,
    render: (value: string, record) => {
      if (record.type === OrderType.MARKET && !value) {
        return "--";
      }

      return <AvgPrice symbol={record.symbol} value={value} />;
    },
    renderPlantText: (value: string, record: any) => {
      return value ? commifyOptional(value) : "--";
    },
  };
}

function cancelBtn(option?: {
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: "",
    type: "action",
    dataIndex: "action",
    width: option?.width,
    className: option?.className,
    align: "right",
    fixed: "end",
    render: (_: string, record: any) => {
      if (record.status === OrderStatus.CANCELLED) {
        return <Renew record={record} />;
      }

      if (
        record.status === OrderStatus.NEW ||
        record.algo_status === OrderStatus.NEW
      ) {
        return <CancelButton order={record} />;
      }

      return null;
    },
  };
}

function pendingTabCancelBtn(option?: {
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: "",
    type: "action",
    dataIndex: "action",
    width: option?.width,
    className: option?.className,
    align: "right",
    fixed: "end",
    render: (_: string, record: any) => {
      return <CancelButton order={record} />;
    },
  };
}

function tpslAction(option?: {
  width?: number;
  className?: string;
}): Column<API.Order> {
  return {
    title: "",
    type: "action",
    dataIndex: "action",
    width: option?.width,
    className: option?.className,
    align: "right",
    fixed: "end",
    render: (_: string, record: any) => {
      return (
        <Flex gap={3}>
          <TP_SLEditButton order={record} />
          <CancelButton order={record} />
        </Flex>
      );
    },
  };
}

function compareNumbers(a: number, b: number): number {
  if (a > b) return 1;
  if (a < b) return -1;
  return 0;
}

// estTotal
function estTotalValue(record: any, isPending: boolean): string {
  if (isPending) {
    return getNotional(record) || "--";
  }

  if (isEntirePositionTPSL(record)) {
    return i18n.t("tpsl.entirePosition");
  }

  return record.total_executed_quantity === 0 ||
    Number.isNaN(record.average_executed_price) ||
    record.average_executed_price === null
    ? "--"
    : `${record.total_executed_quantity * record.average_executed_price}`;
}

function trailingCallback(option?: {
  title?: string;
  width?: number;
  className?: string;
  disableEdit?: boolean;
}): Column<API.Order> {
  return {
    title: option?.title ?? i18n.t("orderEntry.trailing"),
    dataIndex: "callback_value",
    className: option?.className,
    width: option?.width,
    renderPlantText: (value: string, record: any) => {
      const { callback_value, callback_rate } = record;
      const val =
        callback_value || (callback_rate ? `${callback_rate * 100}%` : "--");
      return val?.toString();
    },
    render: (value: string, record: any) => {
      if (isTrailingStopOrder(record)) {
        return (
          <TrailingCallbackCell order={record} disabled={option?.disableEdit} />
        );
      }

      return "--";
    },
  };
}
