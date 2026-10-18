import { useEffect } from "react";
// import { useMarkPriceActions } from "../useMarkPrice/useMarkPriceStore";
import { CalculatorScope } from "../../types";
import { useWS } from "../../useWS";
import { CalculatorService } from "../calculator/calculatorService";

export const useWSObserver = (calculatorService: CalculatorService) => {
  const ws = useWS();
  // const { updateMarkPrice } = useMarkPriceActions();

  useEffect(() => {
    console.log("[PAXEER_NETWORK SDK]: subscribing to ws...");
    /// subscribe to the ws;
    const markPriceSubscription = ws.subscribe("markprices", {
      onMessage: (message: any) => {
        const data: Record<string, number> = Object.create(null);

        for (let index = 0; index < message.length; index++) {
          const element = message[index];
          data[element.symbol] = element.price;
        }

        // updateMarkPrice(data);
        // call the calculator service
        calculatorService.calc(CalculatorScope.MARK_PRICE, data, {
          skipWhenOnPause: true,
        });
      },

      onError: (error: any) => {},
    });

    // const marketSubscription = ws.subscribe("tickers", {
    //   onMessage: (message: any) => {
    //     // console.log("tickers", message);
    //     if (Array.isArray(message)) {
    //       calculatorService.calc(CalculatorScope.MARKET, message, {
    //         skipPending: true,
    //         skipWhenOnPause: true,
    //       });
    //       // calculatorService.calc(CalculatorScope.MARKET, message.splice(0), {
    //       //   skipPending: true,
    //       //   skipWhenOnPause: true,
    //       // });
    //     }
    //   },
    // });

    const indexPriceSubscription = ws.subscribe("indexprices", {
      onMessage: (message: any) => {
        if (!Array.isArray(message)) return;

        const prices: Record<string, number> = Object.create(null);

        for (let index = 0; index < message.length; index++) {
          const element = message[index];
          // NOTICE: force change spot to perp, because there is no perp now
          prices[(element.symbol as string).replace("SPOT", "PERP")] =
            element.price;
        }

        calculatorService.calc(CalculatorScope.INDEX_PRICE, prices, {
          skipWhenOnPause: true,
        });
      },
    });

    return () => {
      markPriceSubscription?.();
      indexPriceSubscription?.();
      // marketSubscription?.();
    };
  }, []);
};
