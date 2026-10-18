import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Text, Tooltip } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { BasicSymbolInfo } from "../../../types/types";
import { MarkPriceView } from "../../base/orderBook/markPrice";
import { MiddlePriceView } from "../../base/orderBook/midPriceView";
import { useOrderBookContext } from "../../base/orderBook/orderContext";

interface DesktopMarkPriceProps {
  markPrice: number;
  lastPrice: number[];
  asks: number[][];
  bids: number[][];
  symbolInfo: BasicSymbolInfo;
}

export const DesktopMarkPrice: FC<DesktopMarkPriceProps> = (props) => {
  const { markPrice = 0, lastPrice, asks, bids, symbolInfo } = props;
  const { showTotal } = useOrderBookContext();

  return (
    <div className="oui-flex oui-flex-row oui-ps-3 oui-tabular-nums oui-justify-between oui-text-base-contrast-80 oui-text-xs oui-relative oui-cursor-pointer oui-py-[6px]">
      <div
        className={cn(
          "oui-basis-7/12 oui-flex oui-flex-row oui-items-center oui-me-2 oui-justify-between",
          showTotal && "oui-basis-5/12",
        )}
      >
        <MiddlePriceView
          markPrice={markPrice}
          lastPrice={lastPrice}
          quote_dp={symbolInfo.quote_dp}
          className="oui-text-base"
        />
        <MarkPriceView markPrice={markPrice} quote_dp={symbolInfo.quote_dp} />
      </div>
      <div
        className={cn(
          "oui-basis-5/12 oui-flex oui-items-center oui-fex-row oui-overflow-hidden oui-relative oui-justify-end",
          showTotal && "oui-basis-7/12",
          "oui-pe-3",
        )}
      >
        <Spread asks={asks} bids={bids} />
      </div>
    </div>
  );
};

const Spread: FC<{
  asks: number[][];
  bids: number[][];
}> = (props) => {
  const { asks, bids } = props;

  const { t } = useTranslation();

  const spread = useMemo(() => {
    if (bids.length === 0 && asks.length === 0) {
      return 0;
    }
    const bidRaw = bids[0]?.[0];
    const bid1 =
      bidRaw === undefined || Number.isNaN(bidRaw)
        ? new Decimal(0)
        : new Decimal(bidRaw);

    const asksReversed = [...asks].reverse();
    const index = asksReversed.findIndex((item) => !Number.isNaN(item[0]));
    let ask1 = new Decimal(0);
    if (index !== -1) {
      const askRaw = asksReversed[index][0];
      ask1 = Number.isNaN(askRaw) ? new Decimal(0) : new Decimal(askRaw);
    }

    const mid = ask1.add(bid1).div(2);
    if (mid.isZero()) {
      return 0;
    }
    const dValue = ask1.sub(bid1).div(mid);
    return dValue
      .mul(1_000_000)
      .add(0.1)
      .toDecimalPlaces(0, Decimal.ROUND_CEIL)
      .div(10_000)
      .toNumber();
  }, [asks, bids]);

  return (
    <div>
      <Tooltip
        content={t("trading.orderBook.spreadRatio.tooltip")}
        className="oui-max-w-[240px]"
      >
        <Text
          size="2xs"
          intensity={36}
          className={
            "oui-cursor-pointer oui-underline oui-decoration-dashed oui-decoration-1 oui-underline-offset-4 oui-decoration-base-contrast-36"
          }
        >
          {`${spread}%`}
        </Text>
      </Tooltip>
    </div>
  );
};
