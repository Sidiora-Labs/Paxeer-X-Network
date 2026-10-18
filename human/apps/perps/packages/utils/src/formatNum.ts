import Decimal from "./decimal";

enum FormatNumType {
  pnl,
  notional,
  roi,
  assetValue,
  collateral,
}

function formatNum(
  type: FormatNumType,
  dp: number = 2,
  num?: number | Decimal | string,
  rm?: number,
): Decimal | undefined {
  // parse to decimal
  const decimalNum = parseToDecimal(num);

  // if parse to decimal failed, return fallback
  if (!decimalNum) {
    return undefined;
  }

  // check if the number is greater than 0
  const isMoreThanZero = decimalNum.greaterThan(0);

  // format the number based on the type
  switch (type) {
    case FormatNumType.pnl:
    case FormatNumType.roi:
      const innerRm =
        rm ?? (isMoreThanZero ? Decimal.ROUND_DOWN : Decimal.ROUND_UP);
      return format(decimalNum, dp, innerRm);
    case FormatNumType.notional:
      return format(decimalNum, dp, rm ?? Decimal.ROUND_DOWN);
    case FormatNumType.assetValue:
      return format(decimalNum, dp, rm ?? Decimal.ROUND_DOWN);
    case FormatNumType.collateral:
      return format(decimalNum, dp, rm ?? Decimal.ROUND_DOWN);
  }
}

// format the number to the number of decimal places
function format(num: Decimal, dp: number, rm: number) {
  return num.toDecimalPlaces(dp, rm);
}

function parseToDecimal(num?: number | Decimal | string): Decimal | undefined {
  try {
    if (num instanceof Decimal) {
      return num;
    }
    if (!num) {
      return undefined;
    }
    if (typeof num === "number") {
      return new Decimal(num);
    }
    if (typeof num === "string") {
      if (!num.trim()) {
        return undefined;
      }
      return new Decimal(num);
    }
    return num;
  } catch (error) {
    return undefined;
  }
}

// export the formatNum with namespace
type FormatNumWithNamespace = typeof formatNum & {
  pnl: (num?: number | Decimal | string) => Decimal | undefined;
  notional: (num?: number | Decimal | string) => Decimal | undefined;
  roi: (num?: number | Decimal | string, dp?: number) => Decimal | undefined;
  assetValue: (num?: number | Decimal | string) => Decimal | undefined;
  collateral: (num?: number | Decimal | string) => Decimal | undefined;
};

const formatNumWithNamespace = formatNum as FormatNumWithNamespace;

// add namespace method to formatNum
formatNumWithNamespace.pnl = (num?: number | Decimal | string) => {
  return formatNum(FormatNumType.pnl, 2, num);
};

formatNumWithNamespace.notional = (num?: number | Decimal | string) => {
  return formatNum(FormatNumType.notional, 2, num);
};

formatNumWithNamespace.roi = (num?: number | Decimal | string, dp?: number) => {
  return formatNum(FormatNumType.roi, dp ?? 4, num);
};

formatNumWithNamespace.assetValue = (num?: number | Decimal | string) => {
  return formatNum(FormatNumType.assetValue, 2, num);
};

formatNumWithNamespace.collateral = (num?: number | Decimal | string) => {
  return formatNum(FormatNumType.collateral, 2, num);
};

export { formatNumWithNamespace as formatNum };
