/**
 * Negates a backend fee value for display.
 * Normalizes -0 to 0 so a zero fee never renders as "-0".
 */
export const negateFee = (value?: number): number | undefined => {
  if (value == null) return undefined;
  return value === 0 ? 0 : -value;
};
