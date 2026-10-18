import { OrderInterface, SideType } from "../type";
import { OrderStatus } from "@paxeer-network/types";
import {startOfSecond, startOfMinute, startOfHour, startOfDay, startOfYear, startOfWeek, startOfMonth} from 'date-fns';

const IntervalMap: { [interval: string]: { startOf: string; period: number } } = {
  1: { startOf: 'minute', period: 0 },
  3: { startOf: 'hour', period: 3 * 60 * 1000 },
  5: { startOf: 'hour', period: 5 * 60 * 1000 },
  15: { startOf: 'hour', period: 15 * 60 * 1000 },
  30: { startOf: 'hour', period: 30 * 60 * 1000 },
  60: { startOf: 'hour', period: 0 },
  120: { startOf: 'day', period: 2 * 60 * 60 * 1000 },
  240: { startOf: 'day', period: 4 * 60 * 60 * 1000 },
  480: { startOf: 'day', period: 8 * 60 * 60 * 1000 },
  720: { startOf: 'day', period: 12 * 60 * 60 * 1000 },
  D: { startOf: 'day', period: 0 }, // default day interval shown as D instead of 1D
  '1D': { startOf: 'day', period: 0 },
  '3D': { startOf: 'year', period: 3 * 24 * 60 * 60 * 1000 },
  '5D': { startOf: 'year', period: 5 * 24 * 60 * 60 * 1000 },
  '1W': { startOf: 'week', period: 0 },
  '1M': { startOf: 'month', period: 0 },
};

function collectionOrders(order: OrderInterface, collection:any,interval: string, orderList: any[], ) {
  const { startOf, period } = IntervalMap[interval];

  const time = new Date(order.updated_time).getTime();
  let base = startOfSecond(time).getTime();
  if (startOf === 'minute') {
    base = startOfMinute(time).getTime();
  } else if (startOf === 'hour') {
    base =startOfHour(time).getTime();
  } else if (startOf === 'day') {
    base = startOfDay(time).getTime();
  } else if (startOf === 'month') {
    base = startOfMonth(time).getTime();
  } else if (startOf === 'year') {
    base = startOfYear(time).getTime();
  } else if (startOf === 'week') {
    base = startOfWeek(time).getTime();
  } else if (startOf === 'month'){
    base = startOfMonth(time).getTime();
  }


  const group = period === 0 ? base : Math.floor((time - base) / period) * period + base;

  if (!collection[group]) {
    collection[group] = { [SideType.BUY]: [], [SideType.SELL]: [] };
  }

  if (collection[group][order.side].length < 5) {
    collection[group][order.side].push(order);
    orderList.push(order);
  }
}

export const limitOrdersByInterval = (orders: OrderInterface[], interval: string) => {
  const res: OrderInterface[] = [];
  const collection: any = {};

  if (!IntervalMap[interval]) {
    return [];
  }

  orders.forEach((order) => {
    if (order.child_orders) {
      for (const child of order.child_orders) {
        if (child.is_activated && child.algo_status=== OrderStatus.FILLED) {
          collectionOrders(child, collection, interval, res);

        }
      }
    } else {
      collectionOrders(order, collection, interval, res);
    }

  });


  return res;
};
