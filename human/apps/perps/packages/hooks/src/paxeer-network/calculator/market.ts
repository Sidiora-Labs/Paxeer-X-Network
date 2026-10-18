import { WSMessage } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { Calculator, CalculatorCtx, CalculatorScope } from "../../types";
import { useMarketStore } from "../useMarket/market.store";
import { BaseCalculator } from "./baseCalculator";

class MarketCalculator extends BaseCalculator<any> {
  name: string = "marketCalculator";

  calc(scope: CalculatorScope, data: any, ctx: CalculatorCtx) {
    const { markets } = ctx;
    if (markets === null) return;

    if (Array.isArray(data)) {
      return data.map((ticker: WSMessage.Ticker) => {
        const market = markets[ticker.symbol];
        const data = {
          ...market,
          "24h_close": ticker.close,
          "24h_high": ticker.high,
          "24h_low": ticker.low,
          "24h_open": ticker.open,
          /**
           * @deprecated
           */
          "24h_volumn": ticker.volume,
          "24h_volume": ticker.volume,
          "24h_amount": ticker.amount,
          change: 0,
          // change: ticker.change,
        };

        if (ticker.close !== undefined && ticker.open !== undefined) {
          data["change"] = new Decimal(ticker.close)
            .minus(ticker.open)
            .div(ticker.open)
            .toNumber();
        }

        return data;
      });
    }

    return null;
  }

  update(data: any, scope: CalculatorScope) {
    if (!data) return;
    useMarketStore.getState().actions.updateMarket(data);
  }
}

export { MarketCalculator };
