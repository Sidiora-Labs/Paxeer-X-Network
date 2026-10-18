import { useMemo } from "react";
import { API, AlgoOrderRootType, OrderStatus } from "@paxeer-network/types";
import { withTPSLProvenance } from "@paxeer-network/utils";

export const useFormatOrderHistory = (data: API.AlgoOrderExt[]) => {
  const formattedData = useMemo(() => {
    const _data: API.AlgoOrderExt[] = [];

    for (let index = 0; index < data.length; index++) {
      const element = withTPSLProvenance(data[index]);
      // console.log("element", element);
      if (
        element.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL ||
        element.algo_type === AlgoOrderRootType.TP_SL
      ) {
        if (
          element.algo_status !== OrderStatus.FILLED &&
          element.algo_status !== OrderStatus.PARTIAL_FILLED
        ) {
          for (let j = 0; j < element.child_orders.length; j++) {
            const e = element.child_orders[j];
            if (!e.is_activated || !e.trigger_price) {
              continue;
            }
            _data.push({
              ...e,
              parent_algo_type: element.algo_type,
              margin_mode: e.margin_mode ?? element.margin_mode,
            } as API.AlgoOrderExt);
          }
        } else {
          // if order is filled then show only the filled order
          for (let j = 0; j < element.child_orders.length; j++) {
            const e = element.child_orders[j];
            if (
              !!e &&
              (e.algo_status === OrderStatus.FILLED ||
                e.algo_status === OrderStatus.PARTIAL_FILLED)
            ) {
              _data.push({
                ...e,
                parent_algo_type: element.algo_type,
                margin_mode: e.margin_mode ?? element.margin_mode,
              } as API.AlgoOrderExt);
            }
          }
        }
      } else {
        _data.push(element);
      }
    }

    return _data;

    // data.map((item) => {});
  }, [data]);

  return formattedData;
};
