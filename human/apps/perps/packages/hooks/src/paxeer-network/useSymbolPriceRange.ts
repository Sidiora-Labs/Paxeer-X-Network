import { Decimal } from "@paxeer-network/utils";
import { useSymbolsInfo } from "./useSymbolsInfo";
import { useMarkPricesStream } from "./useMarkPricesStream";
import { useEffect, useMemo, useState } from "react";

// Define the PriceRange type to represent a range of prices
export type PriceRange = {
    min: number,
    max: number,
};

/**
 * Get the price range for the specified symbol with an optional price
 *
 * @param symbol - The symbol to get the price range for
 * @param price - Optional parameter to set the price
 * @returns PriceRange | undefined - Returns the PriceRange representing the price range or undefined
 */
export const useSymbolPriceRange = (symbol: string, side: "BUY" | "SELL", price?: number): PriceRange | undefined => {
    const config = useSymbolsInfo();
    const priceRange = config?.[symbol]("price_range");
    const priceScrope = config?.[symbol]("price_scope");
    const { data: prices } = useMarkPricesStream();
    const markPrice = price || prices?.[symbol];

    /// max mark_price_i * (1+price_range)
    /// min mark_price_i * (1-price_range)

    const range = useMemo(() => {
        if (config === undefined || markPrice === undefined) {
            return undefined;
        }

        if (priceRange === undefined || Number.isNaN(markPrice)) {
            return undefined;
        }

        if (side === "BUY") {
            return {
                max: new Decimal(markPrice).mul((1 + priceRange)).toNumber(),
                min: new Decimal(markPrice).mul((1 - priceScrope)).toNumber(),
            };
        }

        return {
            max: new Decimal(markPrice).mul((1 + priceScrope)).toNumber(),
            min: new Decimal(markPrice).mul((1 - priceRange)).toNumber(),
        };
    }, [symbol, side, priceRange, markPrice]);

    return range;
}
