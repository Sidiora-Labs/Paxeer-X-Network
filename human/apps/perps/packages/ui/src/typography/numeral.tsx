import React, { FC, useMemo } from "react";
import { cnBase } from "tailwind-variants";
import { Text, TextProps } from "./text";
import { RoundingMode, parseNumber } from "./utils";

export type NumeralRule = "percentages" | "price" | "human";

export const isNumeralRule = (rule: string): rule is NumeralRule => {
  return ["percentages", "price", "human"].includes(rule);
};

export type NumeralProps = TextProps & {
  rule?: NumeralRule;

  /**
   * decimal place, default 2 digits
   * @default 2
   */
  dp?: number;

  /**
   * Whether to ignore the decimal place, if true then direct output, the default is false
   */
  ignoreDP?: boolean;

  /**
   * The number of decimal places to round to.
   * tick is obtained directly from the paxeer-network API and is used to calculate the number of decimal places for prices.
   * If both dp and tick are passed, dp takes priority.
   * Format: 0.00001
   */
  tick?: number;
  /**
   * Rounding mode
   * The method of rounding the decimal digits after the decimal point, options are ceil, floor, round, aligns with Math.ceil, Math.floor, Math.round.
   * @default floor
   */
  rm?: RoundingMode;
  //   truncate?: "ceil" | "floor" | "round" | "truncate";

  /**
   * The number to be formatted
   */
  children: number | string;

  className?: string;

  unitClassName?: string;

  coloring?: boolean;

  loading?: boolean;

  suffix?: React.ReactNode;
  prefix?: React.ReactNode;

  unit?: string;
  currency?: string;

  /**
   * Whether to display as *****
   */
  visible?: boolean;
  /**
   * Whether to pad with 0
   * @default true
   */
  padding?: boolean;

  /**
   * Whether to show the + or - sign
   */
  showIdentifier?: boolean;

  /**
   * Placeholder when children is not valid number
   */
  placeholder?: string;

  /**
   * custom masking element when visible is false
   */
  masking?: React.ReactNode | string;

  /**
   * Custom + or - sign
   */
  identifiers?: {
    loss?: React.ReactNode;
    profit?: React.ReactNode;
  };
};

export const Numeral: FC<NumeralProps> = (props) => {
  const {
    rule = "price",
    coloring,
    dp,
    tick,
    suffix,
    prefix,
    visible,
    unit,
    currency,
    rm,
    padding = true,
    showIdentifier = false,
    identifiers,
    className,
    unitClassName,
    placeholder,
    masking,
    ignoreDP,
    ...rest
  } = props;
  // TODO: check precision

  const num = Number(props.children);

  const child = useMemo(() => {
    if (props.children === "-") return props.children;
    if (isNaN(num)) return placeholder ?? "--";

    if (typeof visible !== "undefined" && !visible) return masking ?? "*****";

    if (ignoreDP) {
      if (!!props.showIdentifier) {
        return Math.abs(num).toString();
      }

      return num;
    }

    return parseNumber(num, {
      rule,
      dp,
      tick,
      rm,
      padding,
      abs: showIdentifier,
    });
  }, [num, visible, tick, dp]);

  // console.log("numeral", child, props.showIdentifier);

  const defaultColor = rest.color || "inherit";

  const colorName = useMemo(() => {
    if (!coloring) return defaultColor;
    if (typeof visible !== "undefined" && !visible) return defaultColor;

    if (Number.isNaN(num)) {
      // console.warn(`if coloring, value is need number: ${props.value}`);
      return defaultColor;
    }

    if (num === 0) return "neutral";
    if (num < 0) return "lose";

    return "profit";
  }, [coloring, num, rest.color, props.visible]);

  // console.log("parseNumber", child, props.showIdentifier);

  const identifier = useMemo(() => {
    if (!showIdentifier || Number.isNaN(num) || num === 0) return null;
    if (typeof visible !== "undefined" && !visible) return null;

    if (num < 0) {
      if (identifiers?.loss) return identifiers!.loss;
      // @ts-ignore
      //   return <Minus size={12} />;
      return <span>-</span>;
    }

    if (identifiers?.profit) return identifiers!.profit;
    // @ts-ignore
    // return <Plus size={12} />;
    return <span>+</span>;
  }, [num, props.visible, showIdentifier]);

  const childWithUnit = useMemo(() => {
    if (
      typeof suffix === "undefined" &&
      typeof prefix === "undefined" &&
      typeof unit === "undefined" &&
      typeof currency === "undefined" &&
      !showIdentifier
    ) {
      return child;
    }

    const suffixEle = suffix ? (
      typeof suffix === "string" ? (
        <span>{suffix}</span>
      ) : (
        suffix
      )
    ) : undefined;

    const unitEle = unit ? (
      <span className={cnBase("paxeer-network-numeral-unit", unitClassName)}>
        {unit}
      </span>
    ) : undefined;

    const prefixEle = prefix ? (
      prefix
    ) : currency ? (
      <span>{currency}</span>
    ) : undefined;

    const child_unit = (
      <>
        {child}
        {unitEle}
      </>
    );

    return (
      <>
        {prefixEle}
        {typeof identifier !== "undefined" ? (
          <span>
            {identifier}
            <span>{child_unit}</span>
          </span>
        ) : (
          <span>{child_unit}</span>
        )}
        {/*{unitEle}*/}
        {suffixEle}
      </>
    );
  }, [child, suffix, unit, prefix, identifier, unitClassName]);

  return (
    <Text
      {...rest}
      color={colorName}
      dir="ltr"
      className={cnBase("oui-tabular-nums", className)}
    >
      {childWithUnit}
    </Text>
  );
};
