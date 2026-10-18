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

export class TPSLPositionOrderCreator extends BaseAlgoOrderCreator<
  AlgoOrderEntity<AlgoOrderRootType.POSITIONAL_TP_SL>
> {
  type = AlgoOrderRootType.POSITIONAL_TP_SL as unknown as OrderType;

  create(
    values: AlgoOrderEntity<AlgoOrderRootType.POSITIONAL_TP_SL>,
    config: ValuesDepConfig,
  ) {
    const side = values.side === OrderSide.BUY ? OrderSide.SELL : OrderSide.BUY;
    const child_orders = (["tp", "sl"] as const).flatMap((leg) => {
      const trigger = values[`${leg}_trigger_price`];
      if (trigger === undefined) return [];
      const type = resolveTPSLOrderType(
        values[`${leg}_order_type`],
        values[`${leg}_order_price`],
        true,
      );
      const active = !!trigger;
      const child: any = {
        algo_type:
          leg === "tp" ? AlgoOrderType.TAKE_PROFIT : AlgoOrderType.STOP_LOSS,
        reduce_only: true,
        side,
        type,
        trigger_price: active
          ? new Decimal(trigger!).todp(config.symbol.quote_dp).toNumber()
          : trigger,
        trigger_price_type: TriggerPriceType.MARK_PRICE,
        symbol: values.symbol,
        is_activated: active,
      };
      if (active && type === OrderType.LIMIT) {
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
      algo_type:
        values.position_type === PositionType.FULL
          ? AlgoOrderRootType.POSITIONAL_TP_SL
          : AlgoOrderRootType.TP_SL,
      trigger_price_type: TriggerPriceType.MARK_PRICE,

      symbol: values.symbol,
      child_orders,
      margin_mode: values.margin_mode || MarginMode.CROSS,
    };
  }

  crateUpdateOrder(
    values: AlgoOrderEntity<AlgoOrderRootType.POSITIONAL_TP_SL>,
    oldValue: API.AlgoOrder,
    config: ValuesDepConfig,
  ): [
    { child_orders: AlgoOrderUpdateEntity[] },
    AlgoOrderEntity<AlgoOrderRootType.POSITIONAL_TP_SL>,
  ] {
    const data = this.create(values, config);
    return [
      { child_orders: createTPSLOrderUpdates(data.child_orders, oldValue) },
      data,
    ];
  }
}
