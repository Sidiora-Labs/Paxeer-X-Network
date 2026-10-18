import { Decimal } from "@paxeer-network/utils";

// input a bigint, return value/(10^18) number
export function toOrder(value?: bigint): number | undefined {
  if (value === null || value === BigInt(0)) {
    return value === BigInt(0) ? 0 : undefined;
  }

  const decimalValue = new Decimal(value!.toString()).div(1e18);
  return Number(decimalValue.toFixed(18));
}

// input a number | string, return value*(10^18) string
export function toUint(value?: number | string): string | undefined {
  if (typeof value === "undefined") return undefined;
  const decimalValue = new Decimal(value);

  const result = decimalValue.mul(new Decimal("1e18"));

  return result.toFixed(0);
}
