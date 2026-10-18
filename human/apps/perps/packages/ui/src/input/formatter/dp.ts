import { InputFormatterOptions } from "./inputFormatter";
import { Decimal } from "@paxeer-network/utils";

export const dpFormatter = (
  dp: number,
  config?: {
    /**
     * The rounding mode to use when rounding the number
     * @default Decimal.ROUND_DOWN
     */
    roundingMode?: number;
  }
) => {
  const onBefore = (value: string | number, options: InputFormatterOptions) => {
    if (typeof value === "number") value = value.toString();
    if (!value || value.endsWith(".")) return value;
    // if (config && config.tick !== null) {
    //   return utils.formatNumber(value, config.tick) ?? value;
    // }
    return truncateNumber(value, dp);
    // const { roundingMode = Decimal.ROUND_DOWN } = config ?? {};
    // let d = new Decimal(value);
    // d = d.todp(dp, roundingMode);
    // return d.toString();
  };

  return {
    onRenderBefore: onBefore,
    onSendBefore: onBefore,
  };
};

/**
 * Truncate a number to a specified number of decimal places.
 * @param num The string to be truncated.
 * @param decimalPlaces The number of decimal places to keep.
 * @returns The truncated number as a string.
 */
function truncateNumber(numStr: string, decimalPlaces: number): string {
  // Convert the number to a string
  // const numStr = typeof num ==='number'? num.toString():num;

  // Find the position of the decimal point
  const decimalIndex = numStr.indexOf(".");

  // If there's no decimal point or we don't need to truncate, return the number as is
  if (decimalIndex === -1 || decimalPlaces <= 0) {
    return numStr.split(".")[0];
  }

  // Calculate the end position for the substring
  const endIndex = decimalIndex + decimalPlaces + 1;

  // Return the truncated string
  return numStr.substring(0, endIndex);
}
