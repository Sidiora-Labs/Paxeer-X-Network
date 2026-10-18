import { FC, useEffect, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import {
  Checkbox,
  cn,
  Flex,
  Sheet,
  SheetContent,
  SheetTrigger,
  Spinner,
  Text,
  VectorIcon,
} from "@paxeer-network/ui";
import { BasicSymbolInfo } from "../../../types/types";
import { BuySellRatioBar, BuySellRatio } from "../../base/orderBook";
import {
  ORDERBOOK_MOBILE_COIN_TYPE_KEY,
  OrderBookProvider,
} from "../../base/orderBook/orderContext";
import { BuySellRatioSettings } from "../../desktop/orderBook/buySellRatio";
import { FundingRateWidget } from "../fundingRate";
import { Asks } from "./asks";
import { Bids } from "./bids";
import { DepthSelect } from "./depthSelect";
import { Header } from "./header";
import { MarkPrice } from "./markPrice";

export interface OrderBookProps {
  asks: any[];
  bids: any[];
  markPrice: number;
  lastPrice: number[];
  onItemClick?: (item: number[]) => void;
  depths?: string[];
  activeDepth?: string;
  onDepthChange?: (depth: number) => void;
  //
  autoSize?: boolean;
  level?: number;
  base: string;
  quote: string;

  isLoading?: boolean;

  cellHeight?: number;

  className?: string;
  /** Same key as order book stream / funding APIs (e.g. may include broker suffix). */
  symbol: string;
  symbolInfo: BasicSymbolInfo;
  showBuySellRatio?: boolean;
  setShowBuySellRatio?: (show: boolean) => void;
  buySellRatio?: BuySellRatio | null;
}

export const OrderBook: FC<OrderBookProps> = (props) => {
  const {
    lastPrice,
    markPrice,
    quote,
    base,
    isLoading,
    onDepthChange,
    showBuySellRatio = true,
    setShowBuySellRatio,
    buySellRatio,
  } = props;

  const { t } = useTranslation();
  const [settingsOpen, setSettingsOpen] = useState(false);

  const [coinUnit, setCoinUnit] = useLocalStorage<"qty" | "base" | "quote">(
    ORDERBOOK_MOBILE_COIN_TYPE_KEY,
    "qty",
  );

  return (
    <OrderBookProvider
      cellHeight={props.cellHeight ?? 20}
      onItemClick={props.onItemClick}
      depth={props.activeDepth}
      pendingOrders={EMPTY_LIST}
      showTotal={false}
      symbolInfo={props.symbolInfo}
      showBuySellRatio={showBuySellRatio}
      onShowBuySellRatioChange={setShowBuySellRatio}
    >
      <Flex
        direction={"column"}
        p={2}
        id="oui-orderbook-mobile"
        className={cn(
          "oui-orderBook oui-relative oui-size-full",
          props.className,
        )}
        justify={"start"}
        itemAlign={"start"}
      >
        <Flex justify="between" itemAlign="center" className="oui-w-full">
          <FundingRateWidget symbol={props.symbol} />
          <BuySellRatioSettings
            showBuySellRatio={showBuySellRatio}
            setShowBuySellRatio={setShowBuySellRatio}
          />
        </Flex>
        <Header quote={quote} base={base} />
        <Asks data={props.asks} />
        <MarkPrice lastPrice={lastPrice} markPrice={markPrice} />
        <Bids data={props.bids} />
        <DepthSelect
          depth={props.depths || EMPTY_LIST}
          value={props.activeDepth}
          onChange={onDepthChange}
        />
        {showBuySellRatio && (
          <BuySellRatioBar
            ratio={buySellRatio || null}
            className="oui-px-0 oui-text-[8px] oui-h-6 oui-mt-2"
          />
        )}
        {/* TODO(theme): "oui-bg-base-800/70" is an invalid token (the base scale only defines 1-10) and generates no CSS, so this loading overlay has no background. Remove it or replace with "oui-bg-base-8/70" after visual comparison. */}
        {isLoading && (
          <div className="oui-bg-base-800/70 oui-absolute oui-inset-0 oui-z-10 oui-flex oui-h-full oui-min-h-[420px] oui-items-center oui-justify-center">
            <Spinner />
          </div>
        )}
      </Flex>
    </OrderBookProvider>
  );
};
