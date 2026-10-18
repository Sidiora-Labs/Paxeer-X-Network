import { FormattedText, FormattedTextProps } from "./formatted";
import { GradientText, gradientTextVariants } from "./gradient";
import {
  NumTypePnl,
  NumTypeRoi,
  NumTypeNotional,
  NumTypeAssetValue,
  NumTypeCollateral,
} from "./numType";
import { Numeral } from "./numeral";
import {
  SymbolBadgeText,
  SymbolBadge,
  type SymbolBadgeTextProps,
} from "./symbolBadge";
import { Text as BaseText, textVariants } from "./text";
import type { TextProps } from "./text";

export { Statistic, StatisticLabel, statisticVariants } from "./statistic";

export type { NumeralProps } from "./numeral";
export { parseNumber, formatAddress } from "./utils";

export type TextType = typeof BaseText & {
  formatted: typeof FormattedText;
  symbolBadge: typeof SymbolBadgeText;
  numeral: typeof Numeral;
  gradient: typeof GradientText;
  roi: typeof NumTypeRoi;
  pnl: typeof NumTypePnl;
  notional: typeof NumTypeNotional;
  assetValue: typeof NumTypeAssetValue;
  collateral: typeof NumTypeCollateral;
};

const Text = BaseText as TextType;
Text.formatted = FormattedText;
Text.symbolBadge = SymbolBadgeText;
Text.numeral = Numeral;
Text.gradient = GradientText;
Text.roi = NumTypeRoi;
Text.pnl = NumTypePnl;
Text.notional = NumTypeNotional;
Text.assetValue = NumTypeAssetValue;
Text.collateral = NumTypeCollateral;

export { Text, textVariants, gradientTextVariants, GradientText, SymbolBadge };
export type { TextProps, FormattedTextProps, SymbolBadgeTextProps };
