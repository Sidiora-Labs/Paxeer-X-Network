import { SimpleDI } from "@paxeer-network/core";
import useConstant from "use-constant";
import { useSimpleDI } from "./useSimpleDI";
import {
  CalculatorService,
  CalculatorServiceID,
} from "./paxeer-network/calculator/calculatorService";
import { ShardingScheduler } from "./paxeer-network/calculator/shardedScheduler";
import { PositionCalculator } from "./paxeer-network/calculator/positions";
import { MarkPriceCalculator } from "./paxeer-network/calculator/markPrice";
import { CalculatorScope } from "./types";
import { PortfolioCalculator } from "./paxeer-network/calculator/portfolio";
import { IndexPriceCalculator } from "./paxeer-network/calculator/indexPrice";

export const useCalculatorService = () => {
  const { get } = useSimpleDI<CalculatorService>();
  const calculatorService = useConstant(() => {
    let calculatorService = get(CalculatorServiceID);

    if (!calculatorService) {
      const positionCalculator = new PositionCalculator();
      const portfolioCalculator = new PortfolioCalculator();
      const markPriceCalculator = new MarkPriceCalculator();
      const indexPriceCalculator = new IndexPriceCalculator();
      calculatorService = new CalculatorService(new ShardingScheduler(), [
        [
          CalculatorScope.MARK_PRICE,
          [
            markPriceCalculator,
            positionCalculator,
            portfolioCalculator,
            positionCalculator,
          ],
        ],
        [CalculatorScope.POSITION, [positionCalculator, portfolioCalculator]],
        [CalculatorScope.PORTFOLIO, [portfolioCalculator]],
        // indexPrice
        [
          CalculatorScope.INDEX_PRICE,
          [indexPriceCalculator, positionCalculator],
        ],
      ]);

      SimpleDI.registerByName(CalculatorServiceID, calculatorService);
    }
    return calculatorService;
  });

  return calculatorService;
};
