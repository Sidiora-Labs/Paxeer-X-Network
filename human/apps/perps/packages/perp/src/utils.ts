import { Decimal } from "@paxeer-network/utils";

/**
 * Returns the maximum value among the given Decimal or number values.
 * Similar to Math.max but works with Decimal instances.
 * @param values - Variable number of Decimal instances or numbers to compare
 * @returns The maximum value as a Decimal instance
 */
export const DMax = (...values: (Decimal | number)[]): Decimal => {
  if (values.length === 0) {
    throw new Error("DMax requires at least one argument");
  }

  // Convert all values to Decimal instances
  const decimals = values.map((val) =>
    val instanceof Decimal ? val : new Decimal(val),
  );

  // Find the maximum by comparing each value
  let max = decimals[0];
  for (let i = 1; i < decimals.length; i++) {
    if (decimals[i].gte(max)) {
      max = decimals[i];
    }
  }

  return max;
};
