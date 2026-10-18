import {
  AlgoOrderEntity,
  AlgoOrderRootType,
  AlgoOrderType,
  API,
  MarginMode,
  OrderSide,
  OrderType,
  PositionType,
  TriggerPriceType,
} from "@paxeer-network/types";
import { Decimal, resolveTPSLOrderType } from "@paxeer-network/utils";
import { AlgoOrderUpdateEntity, BaseAlgoOrderCreator } from "./baseAlgoCreator";
import { ValuesDepConfig } from "./interface";
import { createTPSLOrderUpdates } from "./tpslOrderUpdates";

export class TPSLOrderCreator extends BaseAlgoOrderCreator<
  AlgoOrderEntity<AlgoOrderRootType.TP_SL>
> {
  type = OrderType.MARKET;

  create(
    values: AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
    config: ValuesDepConfig,
  ) {
    const side = values.side === OrderSide.BUY ? OrderSide.SELL : OrderSide.BUY;
    // Unlike TPSLPositionOrderCreator, this creator omits legs without a
    // trigger entirely ("" means deactivated there); explicit deactivations
    // are carried separately in crateUpdateOrder.
    const child_orders = (["tp", "sl"] as const).flatMap((leg) => {
      const trigger = values[`${leg}_trigger_price`];
      if (!trigger) return [];
      const type = resolveTPSLOrderType(
        values[`${leg}_order_type`],
        values[`${leg}_order_price`],
        false,
      );
      const child: any = {
        algo_type:
          leg === "tp" ? AlgoOrderType.TAKE_PROFIT : AlgoOrderType.STOP_LOSS,
        reduce_only: true,
        side,
        type,
        trigger_price: new Decimal(trigger)
          .todp(config.symbol.quote_dp)
          .toNumber(),

        symbol: values.symbol,
        is_activated: true,
      };
      if (type === OrderType.LIMIT) {
        const price = values[`${leg}_order_price`];
        if (
          price == null ||
          price === "" ||
          !Number.isFinite(Number(price)) ||
          new Decimal(price).todp(config.symbol.quote_dp).lte(0)
        ) {
          throw new Error(
            "An enabled TP/SL limit order requires a positive finite price",
          );
        }
        child.price = new Decimal(price)
          .todp(config.symbol.quote_dp)
          .toNumber();
      }
      return [child];
    });
    return {
      algo_type: AlgoOrderRootType.TP_SL,
      trigger_price_type: TriggerPriceType.MARK_PRICE,
      reduce_only: true,
      quantity: values.quantity,
      symbol: values.symbol,
      child_orders,
      margin_mode: values.margin_mode || MarginMode.CROSS,
    };
  }

  crateUpdateOrder(
    values: AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
    oldValue: API.AlgoOrder,
    config: ValuesDepConfig,
  ): [
    { child_orders: AlgoOrderUpdateEntity[] },
    AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
  ] {
    const data = this.create(values, config);
    const explicitlyDeactivatedTypes = (["tp", "sl"] as const).flatMap(
      (leg) => {
        const trigger = values[`${leg}_trigger_price`];
        if (trigger === undefined || trigger) return [];
        return [
          leg === "tp" ? AlgoOrderType.TAKE_PROFIT : AlgoOrderType.STOP_LOSS,
        ];
      },
    );
    return [
      {
        child_orders: createTPSLOrderUpdates(
          data.child_orders,
          oldValue,
          values.quantity,
          explicitlyDeactivatedTypes,
        ),
      },
      data,
    ];
  }
}
