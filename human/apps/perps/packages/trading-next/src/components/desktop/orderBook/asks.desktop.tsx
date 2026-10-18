import { FC, useMemo } from "react";
import { injectable } from "@paxeer-network/ui";
import { BasicSymbolInfo } from "../../../types/types";
import { OrderBookCellType } from "../../base/orderBook/types";
import { DesktopListBox } from "./listBox.desktop";

/**
 * Desktop asks list props. `symbolInfo` and `depth` are threaded through
 * {@link InjectableDesktopAsks} so paxeer-network plugins replacing `OrderBook.Desktop.Asks`
 * can render without relying only on context. The default `DesktopAsks` uses `data`
 * and reads symbol/depth from `OrderBookProvider` / `OrderBookContext`.
 */
export interface Props {
  data: number[][];
  /** For plugin replacements; unused by the default asks row list (context carries symbol). */
  symbolInfo: BasicSymbolInfo;
  /** For plugin replacements; active depth key; unused by the default list (context carries depth). */
  depth: string | undefined;
}

export const DesktopAsks: FC<Props> = (props) => {
  const { data } = props;
  const countQty = useMemo(() => {
    let max = Number.NaN;
    const len = data.length;
    let index = 0;

    while (Number.isNaN(max) && index < len) {
      max = data[index][2];
      index++;
    }

    return max;
  }, [data]);

  return (
    <DesktopListBox
      type={OrderBookCellType.ASK}
      data={data}
      countQty={countQty}
    />
  );
};

export const InjectableDesktopAsks = injectable<Props>(
  DesktopAsks,
  "OrderBook.Desktop.Asks",
);
