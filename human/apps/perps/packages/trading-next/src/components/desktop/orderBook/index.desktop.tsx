import { FC, useEffect, useRef, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST } from "@paxeer-network/types";
import { cn, Flex, Grid, Spinner } from "@paxeer-network/ui";
import { BasicSymbolInfo } from "../../../types/types";
import { BuySellRatioBar, BuySellRatio } from "../../base/orderBook";
import {
  ORDERBOOK_COIN_TYPE_KEY,
  OrderBookProvider,
} from "../../base/orderBook/orderContext";
import { InjectableDesktopAsks } from "./asks.desktop";
import { InjectableDesktopBids } from "./bids.desktop";
import { BuySellRatioSettings } from "./buySellRatio";
import { DesktopDepthSelect } from "./depthSelect.desktop";
import { DesktopHeader } from "./header.desktop";
import { DesktopMarkPrice } from "./markPrice.desktop";

export interface DesktopOrderBookProps {
  asks: any[];
  bids: any[];
  markPrice: number;
  lastPrice: number[];
  onItemClick?: (item: number[]) => void;
  depths: string[];
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
  pendingOrders?: number[];
  symbolInfo: BasicSymbolInfo;
  showBuySellRatio?: boolean;
  setShowBuySellRatio?: (show: boolean) => void;
  buySellRatio?: BuySellRatio | null;
}

export const DesktopOrderBook: FC<DesktopOrderBookProps> = (props) => {
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
  const divRef = useRef<HTMLDivElement>(null);

  const [showTotal, setShowTotal] = useState(false);

  const [coinType, setCoinType] = useLocalStorage(
    ORDERBOOK_COIN_TYPE_KEY,
    base,
  );

  useEffect(() => {
    if (coinType !== quote && base) {
      setCoinType(base);
    }
  }, [base, quote]);

  useEffect(() => {
    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const { inlineSize: width } = entry.borderBoxSize[0];
        setShowTotal(width >= 360);
      }
    });

    const targetDiv = divRef.current;

    if (targetDiv) {
      resizeObserver.observe(targetDiv);
    }

    return () => {
      if (targetDiv) {
        resizeObserver.unobserve(targetDiv);
      }
    };
  }, []);

  return (
    <OrderBookProvider
      cellHeight={props.cellHeight ?? 20}
      onItemClick={props.onItemClick}
      depth={props.activeDepth}
      showTotal={showTotal}
      pendingOrders={props.pendingOrders || EMPTY_LIST}
      symbolInfo={props.symbolInfo}
      showBuySellRatio={showBuySellRatio}
      onShowBuySellRatioChange={setShowBuySellRatio}
    >
      <Grid
        cols={1}
        rows={showBuySellRatio ? 6 : 5}
        id="oui-orderbook-desktop"
        ref={divRef}
        className={cn(
          "oui-orderBook",
          "oui-relative oui-size-full",
          showBuySellRatio
            ? "oui-grid-rows-[auto,auto,1fr,auto,1fr,auto]"
            : "oui-grid-rows-[auto,auto,1fr,auto,1fr]",
        )}
      >
        <Flex justify="between" itemAlign="center" className="oui-pe-3">
          <DesktopDepthSelect
            depths={props.depths}
            value={props.activeDepth}
            onChange={onDepthChange}
          />
          <BuySellRatioSettings
            showBuySellRatio={showBuySellRatio}
            setShowBuySellRatio={setShowBuySellRatio}
          />
        </Flex>
        <DesktopHeader quote={quote} base={base} />
        {/* symbolInfo/depth: required by injectable props so plugins replacing Asks/Bids receive them */}
        <InjectableDesktopAsks
          data={[...props.asks]}
          symbolInfo={props.symbolInfo}
          depth={props.activeDepth}
        />
        <DesktopMarkPrice
          lastPrice={lastPrice}
          markPrice={markPrice}
          // ======================================================================
          // ==         🔥🔥🔥🔥🔥🔥🔥 DO NOT MODIFY THIS 🔥🔥🔥🔥🔥🔥🔥              ==
          // ======================================================================
          // Use shallow copy here to avoid mutating the original props array when sorting or modifying inside the component.
          // Note: This only clones the array itself, not the element objects.
          asks={[...props.asks]}
          bids={[...props.bids]}
          symbolInfo={props.symbolInfo}
        />
        <InjectableDesktopBids
          data={[...props.bids]}
          symbolInfo={props.symbolInfo}
          depth={props.activeDepth}
        />
        {showBuySellRatio && (
          <BuySellRatioBar
            ratio={buySellRatio || null}
            className="oui-h-[38px] oui-px-3 oui-text-2xs"
          />
        )}
        {isLoading && (
          <div className="oui-bg-bg-8/70 oui-absolute oui-inset-0 oui-z-10 oui-flex oui-items-center oui-justify-center">
            <Spinner />
          </div>
        )}
      </Grid>
    </OrderBookProvider>
  );
};
