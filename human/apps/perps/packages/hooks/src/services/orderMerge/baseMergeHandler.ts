import { lensIndex, over } from "ramda";
import { WSMessage, API, OrderStatus } from "@paxeer-network/types";
import { IOrderMergeHandler } from "./interface";

export abstract class BaseMergeHandler<
  T extends WSMessage.AlgoOrder[] | WSMessage.Order,
  D extends API.AlgoOrder | API.Order,
> implements IOrderMergeHandler<T, D>
{
  data: D;
  constructor(private message: T) {
    this.data = this.formatOrder(message);
  }
  abstract get status(): string;
  abstract get orderId(): number;
  abstract pre(message: T, prevData?: API.OrderResponse[]): D;
  abstract isFullFilled(): boolean;

  /**
   * format the order data;
   */
  formatOrder(message: T): D {
    const data = this.pre(message) as D & {status?:string;timestamp?:number;child_orders?:API.AlgoOrder[]};
    // if order status is REPLACED, maybe not update status

    if (!data.created_time) {
      data.created_time = (data as any).timestamp;
    }
    if (data.status === OrderStatus.FILLED && !data.updated_time) {
      data.updated_time = data.timestamp!;
    }
    if (data.child_orders && data.child_orders.length) {
      data.child_orders.map((child) => {
        if (
          child.algo_status === OrderStatus.FILLED &&
          child.is_activated &&
          !child.updated_time
        ) {
          child.updated_time = data.timestamp!;
        }
      });
    }
    if (data.type && data.type.endsWith("_ORDER")) {
      data.type = data.type.replace("_ORDER", "");
    }

    return data;
  }

  merge(
    key: string,
    message: T,
    prevData: API.OrderResponse[],
  ): API.OrderResponse[] {
    switch (this.status) {
      case "NEW": {
        if (
          key.startsWith("orders:CANCELLED") ||
          key.startsWith("orders:FILLED") ||
          key.startsWith("orders:REJECTED")
        ) {
          return prevData;
        }
        return this.insetOrUpdate(prevData);
      }
      case "CANCELLED": {
        if (
          key.startsWith("orders:FILLED") ||
          key.startsWith("orders:REJECTED")
        ) {
          return prevData;
        }
        if (
          key.startsWith("orders:NEW") ||
          key.startsWith("orders:INCOMPLETE") ||
          key.startsWith("algoOrders:INCOMPLETE")
        ) {
          return this.remove(prevData);
        }

        if (key.startsWith("orders:CANCELLED")) {
          return this.insert(prevData);
        }

        return this.update(prevData);
      }

      case "REPLACED":
        return this.update(prevData);

      case "FILLED": {
        if (this.orderIsExisting(prevData)) {
          // for new list, remove the order if it exists
          if (
            key.startsWith("orders:INCOMPLETE") ||
            key.startsWith("orders:NEW") ||
            key.startsWith("algoOrders:INCOMPLETE") ||
            // all orders key
            key.startsWith("orders:")
          ) {
            // if fullfilled, remove from the list
            if (this.isFullFilled()) {
              return this.remove(prevData);
            }

            // update
            return this.update(prevData);
          }
        } else {
          // for filled list, insert the order if it doesn't exist

          if (
            key.startsWith("orders:CANCELLED") ||
            key.startsWith("orders:INCOMPLETE") ||
            key.startsWith("orders:NEW") ||
            key.startsWith("algoOrders:INCOMPLETE")
          ) {
            return prevData;
          }
          // if filled/history list:
          return this.insert(prevData);
        }
      }

      default:
        return prevData || [];
    }
  }

  insert(orders?: API.OrderResponse[] | undefined): API.OrderResponse[] {
    const index = lensIndex<API.OrderResponse>(0);
    return over(
      //@ts-ignore
      index,
      (item: any) => ({
        meta: {
          ...item.meta,
          total: item.meta.total + 1,
        },
        rows: [this.data, ...item.rows],
      }),
      orders,
    ) as API.OrderResponse[];
  }

  insetOrUpdate(orders: API.OrderResponse[]): API.OrderResponse[] {
    if (this.orderIsExisting(orders)) {
      return this.update(orders);
    }
    return this.insert(orders);
  }

  update(prevData: API.OrderResponse[]): API.OrderResponse[] {
    const idx = this.findOrderIndex(prevData);
    if (!Array.isArray(idx)) {
      return prevData || [];
    }

    const [index, index2] = idx;

    return prevData.map((item, i) => {
      return {
        meta: { ...item.meta },
        rows: item.rows.map((order, j) => {
          if (index === i && index2 === j) {
            return { ...order, ...this.data };
          }
          return order;
        }),
      };
    });
  }

  remove(prevData: API.OrderResponse[]): API.OrderResponse[] {
    const idx = this.findOrderIndex(prevData);
    if (!Array.isArray(idx)) {
      return prevData;
    }

    const [index, index2] = idx;

    return prevData.map((item, i) => {
      return {
        meta: { ...item.meta, total: item.meta.total - 1 },
        rows:
          index === i ? item.rows.filter((_, i) => i !== index2) : item.rows,
      };
    });
  }

  findOrderIndex(orders: API.OrderResponse[]): number[] | undefined {
    let index: number = 0;
    let index2: number | undefined;

    for (; index < orders.length; index++) {
      // index = idx;
      const item = orders[index];

      for (let idx2 = 0; idx2 < item.rows.length; idx2++) {
        const element = item.rows[idx2];
        if (
          (element as API.AlgoOrder).algo_order_id === this.orderId ||
          (element as API.Order).order_id === this.orderId
        ) {
          index2 = idx2;

          break;
        }
      }
      if (typeof index2 !== "undefined") {
        break;
      }
    }

    if (typeof index2 === "undefined") {
      return;
    }

    return [index, index2];
  }

  orderIsExisting(orders: API.OrderResponse[]): boolean {
    const index = this.findOrderIndex(orders);
    return Array.isArray(index);
  }
}
