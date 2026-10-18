import { FC, useMemo } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { cn, Divider, parseNumber, Text } from "@paxeer-network/ui";
import { getPrecisionByNumber } from "@paxeer-network/utils";
import { BasicSymbolInfo } from "../../../types/types";
import { CellBar, CellBarDirection } from "../../base/orderBook/cellBar";
import {
  ORDERBOOK_COIN_TYPE_KEY,
  useOrderBookContext,
} from "../../base/orderBook/orderContext";
import { OrderBookCellType } from "../../base/orderBook/types";

export interface DesktopOrderBookCellProps {
  background: string;
  maxQty: number;
  price: number;
  quantity: number;
  // size: number;
  count: number;
  accumulated: number;
  accumulatedAmount: number;
  type: OrderBookCellType;
  symbolInfo: BasicSymbolInfo;
  base: string;
  quote: string;
  isHover: boolean;
  currentHover: boolean;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}

export const DesktopOrderBookCell: FC<DesktopOrderBookCellProps> = (props) => {
  const { cellHeight, showTotal, onItemClick, depth, pendingOrders } =
    useOrderBookContext();
  const {
    symbolInfo,
    currentHover,
    accumulated,
    accumulatedAmount,
    count,
    price,
    quantity,
    base,
    quote,
  } = props;

  const { base_dp, quote_dp } = symbolInfo;

  const [coinType] = useLocalStorage<string>(ORDERBOOK_COIN_TYPE_KEY, base);

  const width = Number.isNaN(price) ? 0 : (accumulated / count) * 100;

  const dp = useMemo(() => {
    return getPrecisionByNumber(depth || `${quote_dp}`);
  }, [depth, quote_dp]);

  const totalAmount = Number.isNaN(accumulated)
    ? "-"
    : accumulatedAmount?.toString();

  const isPendingOrder = useMemo(() => {
    const priceStr = parseNumber(price, { dp: dp, padding: true });
    return pendingOrders.some(
      (item) => priceStr === parseNumber(item, { dp: dp, padding: true }),
    );
  }, [pendingOrders, price, dp]);

  return (
    <div
      className="oui-relative oui-flex oui-cursor-pointer oui-flex-row oui-justify-between oui-ps-3 oui-text-xs oui-tabular-nums oui-text-base-contrast-80"
      style={{ height: `${cellHeight}px` }}
      onClick={() => {
        if (Number.isNaN(price) || Number.isNaN(quantity)) {
          return;
        }
        onItemClick?.([price, quantity]);
      }}
      onMouseEnter={props.onMouseEnter}
      onMouseLeave={props.onMouseLeave}
    >
      <div
        className={cn(
          "oui-me-2 oui-flex oui-basis-7/12 oui-flex-row oui-items-center",
          showTotal && "oui-basis-5/12",
        )}
      >
        <div
          className={cn(
            "oui-flex-1 oui-text-start",
            props.type === OrderBookCellType.ASK
              ? "oui-text-trade-loss"
              : "oui-text-trade-profit",
          )}
        >
          <Text.numeral dp={dp} dir="ltr">
            {price}
          </Text.numeral>
        </div>
        <div className="oui-flex-1 oui-text-end oui-text-base-contrast-80">
          <Text.numeral dp={base_dp} dir="ltr">
            {quantity}
          </Text.numeral>
        </div>
      </div>
      <div
        className={cn(
          "oui-fex-row oui-relative oui-flex oui-basis-5/12 oui-items-center oui-justify-end oui-overflow-hidden",
          showTotal && "oui-basis-7/12",
        )}
      >
        {showTotal ? (
          <>
            <div className={cn("oui-flex-1 oui-pe-3 oui-text-end")}>
              <Text.numeral dp={base_dp} className="oui-z-10" dir="ltr">
                {accumulated}
              </Text.numeral>
            </div>
            <div className={cn("oui-flex-1 oui-pe-3 oui-text-end")}>
              <Text.numeral dp={0} className="oui-z-10" dir="ltr">
                {totalAmount}
              </Text.numeral>
            </div>
          </>
        ) : (
          <div className={cn("oui-flex-1 oui-pe-3 oui-text-end")}>
            {coinType === base && (
              <Text.numeral dp={base_dp} className="oui-z-10" dir="ltr">
                {accumulated}
              </Text.numeral>
            )}
            {coinType === quote && (
              <Text.numeral dp={0} className="oui-z-10" dir="ltr">
                {totalAmount}
              </Text.numeral>
            )}
          </div>
        )}
        <CellBar
          width={width}
          direction={CellBarDirection.LEFT_TO_RIGHT}
          className={
            props.type === OrderBookCellType.ASK
              ? "oui-bg-trade-loss/10"
              : "oui-bg-trade-profit/10"
          }
        />
      </div>

      {isPendingOrder && (
        <div
          className={cn(
            "oui-pointer-events-none oui-absolute oui-start-[4px] oui-size-[4px] oui-rounded-full",
            props.type === OrderBookCellType.ASK && "oui-bg-trade-loss",
            props.type === OrderBookCellType.BID && "oui-bg-trade-profit",
          )}
          style={{ top: `${cellHeight / 2 - 2}px` }}
        />
      )}

      {props.isHover && (
        <div className="oui-absolute oui-inset-0 oui-bg-base-contrast-12" />
      )}
      {currentHover && (
        <div
          className={cn(
            "oui-absolute oui-inset-x-0",
            props.type === OrderBookCellType.ASK && "oui-top-0",
            props.type === OrderBookCellType.BID && "oui-bottom-0",
          )}
        >
          <Divider
            lineStyle="dashed"
            className={cn(
              "oui-w-full",
              props.type === OrderBookCellType.BID && "oui-border-trade-profit",
              props.type === OrderBookCellType.ASK && "oui-border-trade-loss",
            )}
          />
        </div>
      )}
    </div>
  );
};
