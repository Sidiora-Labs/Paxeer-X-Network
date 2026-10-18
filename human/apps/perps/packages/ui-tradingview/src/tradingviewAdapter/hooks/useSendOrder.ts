import { useOrderEntry_deprecated } from "@paxeer-network/hooks";
import { OrderSide, OrderType } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { toast } from "@paxeer-network/ui";

export default function useSendOrder(symbol: string) {
  const { onSubmit: _sendMarketOrder } = useOrderEntry_deprecated(
    {
      symbol,
      side: OrderSide.BUY,
      order_type: OrderType.MARKET,
    },
    {
      watchOrderbook: true,
    }
  );
  const _sendOrder = () => {};

  const sendLimitOrder = () => {};

  const sendMarketOrder = (data: any) => {
    console.log("-order DAta", data);
    data.reduce_only = false;
    return _sendMarketOrder(data).catch((e) => {
      console.log("--- e", e);
      toast.error(e);
    });
  };

  return {
    sendLimitOrder,
    sendMarketOrder,
  };
}
