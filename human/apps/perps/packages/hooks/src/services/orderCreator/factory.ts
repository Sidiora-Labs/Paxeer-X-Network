import { OrderType } from "@paxeer-network/types";
import { AlgoOrderRootType } from "@paxeer-network/types";
import { BBOOrderCreator } from "./bboOrderCreator";
import { BracketLimitOrderCreator } from "./bracketLimitOrderCreator";
import { BracketMarketOrderCreator } from "./bracketMarketOrderCreator";
import { FOKOrderCreator } from "./fokCreator";
import { GeneralOrderCreator } from "./generalCreator";
import { OrderCreator } from "./interface";
import { IOCOrderCreator } from "./iocCreator";
import { LimitOrderCreator } from "./limitOrderCreator";
import { MarketOrderCreator } from "./marketOrderCreator";
import { PostOnlyOrderCreator } from "./postOnlyCreator";
import { ScaledOrderCreator } from "./scaledOrderCreator";
import { StopLimitOrderCreator } from "./stopLimitOrderCreator";
import { StopMarketOrderCreator } from "./stopMarketOrderCreator";
import { TPSLOrderCreator } from "./tpslOrderCreator";
import { TPSLPositionOrderCreator } from "./tpslPositionOrderCreator";
import { TrailingStopOrderCreator } from "./trailingStopOrderCreator";

export class OrderFactory {
  static create(
    type: OrderType | AlgoOrderRootType | string,
  ): OrderCreator<any> {
    switch (type) {
      case `${AlgoOrderRootType.BRACKET}:${OrderType.LIMIT}`:
        return new BracketLimitOrderCreator();
      case `${AlgoOrderRootType.BRACKET}:${OrderType.MARKET}`:
        return new BracketMarketOrderCreator();
      case OrderType.LIMIT:
        return new LimitOrderCreator();
      case OrderType.MARKET:
        return new MarketOrderCreator();
      case OrderType.ASK:
      case OrderType.BID:
        return new BBOOrderCreator();
      case OrderType.IOC:
        return new IOCOrderCreator();
      case OrderType.FOK:
        return new FOKOrderCreator();
      case OrderType.POST_ONLY:
        return new PostOnlyOrderCreator();
      case OrderType.STOP_LIMIT:
        return new StopLimitOrderCreator();
      case OrderType.STOP_MARKET:
        return new StopMarketOrderCreator();
      case OrderType.SCALED:
        return new ScaledOrderCreator();
      case OrderType.TRAILING_STOP:
        return new TrailingStopOrderCreator();

      // algo order
      case AlgoOrderRootType.TP_SL:
        return new TPSLOrderCreator();
      case AlgoOrderRootType.POSITIONAL_TP_SL:
        return new TPSLPositionOrderCreator();

      default:
        return new GeneralOrderCreator();
    }
  }
}
