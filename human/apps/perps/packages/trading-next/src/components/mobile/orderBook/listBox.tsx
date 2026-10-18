import { FC } from "react";
import { Box } from "@paxeer-network/ui";
import { useOrderBookContext } from "../../base/orderBook/orderContext";
import { OrderBookCellType } from "../../base/orderBook/types";
import { OrderBookCell } from "./cell";

interface OrderBookListProps {
  type: OrderBookCellType;
  data: number[][];
  countQty: number;
}

export const ListBox: FC<OrderBookListProps> = (props) => {
  const { data } = props;
  const { mode } = useOrderBookContext();
  return (
    <Box className="oui-orderBook-list oui-order-book-list oui-flex oui-w-full oui-flex-col oui-gap-px">
      {data.map((item, index) => {
        return (
          <OrderBookCell
            key={`item-${index}`}
            background={""}
            price={item[0]}
            quantity={item[1]}
            accumulated={item[2]}
            accumulatedAmount={item[3]}
            count={props.countQty}
            type={props.type}
            mode={mode}
          />
        );
      })}
    </Box>
  );
};
