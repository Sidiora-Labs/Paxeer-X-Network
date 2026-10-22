import BigNumber from 'bignumber.js';

import { DEFAULT_ACCURACY, formatBnValue } from 'ui/shared/value/utils';

export const PRICE_DECIMALS = 18;

export const formatPrice = (raw: string) => formatBnValue({
  value: new BigNumber(raw).shiftedBy(-PRICE_DECIMALS),
  accuracy: DEFAULT_ACCURACY,
});

// Percentage move from the previous price to the current one; null when there is no base to compare.
export const priceChange = (current: string, previous: string | undefined): BigNumber | null => {
  if (previous === undefined) {
    return null;
  }

  const base = new BigNumber(previous);

  if (base.isZero()) {
    return null;
  }

  return new BigNumber(current).minus(base).div(base).times(100);
};

export const formatChange = (change: BigNumber) => {
  const rounded = change.dp(2);
  const sign = rounded.isGreaterThan(0) ? '+' : '';

  return `${ sign }${ rounded.toFormat(2) }%`;
};
