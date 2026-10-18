/**
 * current account leverage
 */
export function currentLeverage(totalMarginRatio: number) {
  if (totalMarginRatio === 0) {
    return 0;
  }
  return 1 / totalMarginRatio;
}
