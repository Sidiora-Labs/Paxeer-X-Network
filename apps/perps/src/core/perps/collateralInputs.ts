import { safeParseUnits } from "./amounts";
import { COLLATERAL_TOKENS } from "./config";

export function toCollateralUnits(text: string): bigint {
  return safeParseUnits(text, COLLATERAL_TOKENS.usdl.decimals);
}
