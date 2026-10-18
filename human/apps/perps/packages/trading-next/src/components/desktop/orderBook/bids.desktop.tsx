import { FC, useMemo } from "react";
import { injectable } from "@paxeer-network/ui";
import { BasicSymbolInfo } from "../../../types/types";
import { OrderBookCellType } from "../../base/orderBook/types";
import { DesktopListBox } from "./listBox.desktop";

/**
 * Desktop bids list props. `symbolInfo` and `depth` are threaded through
 * {@link InjectableDesktopBids} so paxeer-network plugins replacing `OrderBook.Desktop.Bids`
 * can render without relying only on context. The default `DesktopBids` uses `data`
 * and reads symbol/depth from `OrderBookProvider` / `OrderBookContext`.
 */
export interface Props {
  data: number[][];
  /** For plugin replacements; unused by the default bids row list (context carries symbol). */
  symbolInfo: BasicSymbolInfo;
  /** For plugin replacements; active depth key; unused by the default list (context carries depth). */
  depth: string | undefined;
}

export const DesktopBids: FC<Props> = (props) => {
  const { data } = props;
  const countQty = useMemo(() => {
    let max = Number.NaN;
    // let len = data.length;
    let index = data.length - 1;

    while (Number.isNaN(max) && index > 0) {
      max = data[index][2];
      index--;
    }

    return max;
    // return data.length > 0 ? data[data.length - 1][2] : 0;
  }, [data]);
  return (
    <DesktopListBox
      type={OrderBookCellType.BID}
      data={data}
      countQty={countQty}
    />
  );
};

export const InjectableDesktopBids = injectable<Props>(
  DesktopBids,
  "OrderBook.Desktop.Bids",
);
