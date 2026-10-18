import { numberToHumanStyle } from "./decimal";

/**
 * Format trading pair symbol *
 * Format symbols like "PERP_BTC_USDT" according to the specified format
 *
 * @param symbol - Original trading pair symbol in format "type_base_quote" (e.g., "PERP_BTC_USDT")
 * @param formatString - Format string template, defaults to "base"
 *                    - Supports the following placeholders:
 *                    - "type": Trading type (e.g., "PERP")
 *                    - "base": Base currency (e.g., "BTC")
 *                    - "quote": Quote currency (e.g., "USDT")
 *
 * @returns Formatted string, returns empty string if input is empty
 *
 * @example
 * ```typescript
 * formatSymbol("PERP_BTC_USDT")                    // "BTC"
 * formatSymbol("PERP_BTC_USDT", "base")            // "BTC"
 * formatSymbol("PERP_BTC_USDT", "base-type")      // "BTC-PERP"
 * formatSymbol("SPOT_ETH_USDC", "base-quote")      // "ETH-USDC"
 * ```
 */
export function formatSymbol(
  symbol: string,
  formatString: string = "base-type",
) {
  if (!symbol) {
    return "";
  }

  // Split symbol by "_" to get each component
  const arr = symbol.split("_");
  const type = arr[0]; // Trading type (e.g., PERP, SPOT)
  const base = arr[1]; // Base currency (e.g., BTC, ETH)
  const quote = arr[2]; // Quote currency (e.g., USDT, USDC)

  // Use template string to replace placeholders and return formatted result
  return formatString
    .replace("type", type)
    .replace("base", base)
    .replace("quote", quote);
}

/**
 * Optimize symbol text display by converting leading numbers to human-readable format
 *
 * This function matches numbers at the beginning of a symbol string and converts them
 * to abbreviated format (K, M, B, T) for better readability.
 *
 * @param symbol - The symbol string to optimize (e.g., "1000000BABYDOGE")
 * @param decimalPlaces - Number of decimal places for the abbreviated number (default: 0)
 * @returns Optimized symbol string (e.g., "1MBABYDOGE")
 *
 * @example
 * ```typescript
 * // Basic usage - convert large numbers to abbreviated format
 * optimizeSymbolDisplay("1000000BABYDOGE")     // "1MBABYDOGE"
 * optimizeSymbolDisplay("5000ETH")             // "5KETH"
 * optimizeSymbolDisplay("1500000000SHIB")      // "1BSHIB"
 * optimizeSymbolDisplay("2000000000000TOKEN")  // "2TTOKEN"
 *
 * // With decimal places
 * optimizeSymbolDisplay("1500000TOKEN", 1)     // "1.5MTOKEN"
 * optimizeSymbolDisplay("2750000COIN", 2)      // "2.75MTOKEN"
 *
 * // Edge cases - no modification needed
 * optimizeSymbolDisplay("BITCOIN")             // "BITCOIN" (no leading number)
 * optimizeSymbolDisplay("123.45TOKEN")         // "123.45TOKEN" (less than 1000)
 * optimizeSymbolDisplay("999COIN")             // "999COIN" (less than 1000)
 *
 * // Usage in React component
 * <SymbolText size="sm" optimizeDisplay={true} decimalPlaces={1}>
 *   1000000BABYDOGE
 * </SymbolText>
 * // Renders: "1MBABYDOGE" with token icon
 * ```
 */
export function optimizeSymbolDisplay(
  symbol: string,
  decimalPlaces: number = 0,
): string {
  if (!symbol) {
    return "";
  }

  // Regular expression to match numbers at the beginning of the string
  // Matches: optional decimal numbers starting from the beginning
  const numberRegex = /^(\d+(?:\.\d+)?)/;
  const match = symbol.match(numberRegex);

  if (!match) {
    // No leading number found, return original symbol
    return symbol;
  }

  const numberPart = match[1]; // The matched number string
  const textPart = symbol.slice(numberPart.length); // The remaining text after the number
  const numericValue = parseFloat(numberPart);

  // Only convert numbers >= 1000 to abbreviated format
  if (numericValue < 1000) {
    return symbol;
  }

  // Use the imported numberToHumanStyle function from decimal utils
  const abbreviatedNumber = numberToHumanStyle(numericValue, decimalPlaces);

  return abbreviatedNumber + textPart;
}
