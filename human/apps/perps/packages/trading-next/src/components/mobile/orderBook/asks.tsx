import { FC, useMemo } from "react";
import { OrderBookCellType } from "../../base/orderBook/types";
import { ListBox } from "./listBox";

export interface Props {
  data: number[][];
}
export const Asks: FC<Props> = (props) => {
  const { data } = props;
  const countQty = useMemo(() => {
    const len = data.length;
    let max = Number.NaN;

    let index = 0;

    while (Number.isNaN(max) && index < len) {
      max = data[index][2];
      index++;
    }

    return max;
  }, [data]);
  return (
    <ListBox type={OrderBookCellType.ASK} data={data} countQty={countQty} />
  );
};
