import { FC } from "react";
import { Flex } from "@paxeer-network/ui";
import { MarkPriceView } from "../../base/orderBook/markPrice";
import { MiddlePriceView } from "../../base/orderBook/midPriceView";
import { useOrderBookContext } from "../../base/orderBook/orderContext";

interface MarkPriceProps {
  markPrice: number;
  lastPrice: number[];
}

export const MarkPrice: FC<MarkPriceProps> = (props) => {
  const { symbolInfo } = useOrderBookContext();
  const { quote_dp } = symbolInfo;

  return (
    <Flex
      id="oui-order-book-mark-price"
      className="oui-orderBook-markPrice oui-py-[6px]"
      width={"100%"}
      justify={"between"}
    >
      <MiddlePriceView
        markPrice={props.markPrice}
        lastPrice={props.lastPrice}
        quote_dp={quote_dp}
        className="oui-text-sm"
        iconSize={14}
      />
      <MarkPriceView
        markPrice={props.markPrice}
        quote_dp={quote_dp}
        className="oui-text-2xs"
        iconSize={12}
      />
    </Flex>
  );
};
