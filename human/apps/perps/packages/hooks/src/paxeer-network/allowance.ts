import { SDKError } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

const allowanceRetryDelays = [500, 1000, 2000, 2000, 2000] as const;

export type AllowanceComparison = "gte" | "eq";

type WaitForAllowanceOptions = {
  readAllowance: () => Promise<string>;
  expectedAmount: string;
  comparison?: AllowanceComparison;
  onRead?: (allowance: string) => void;
  onSatisfied?: (allowance: string) => Promise<void>;
};

export async function waitForAllowance({
  readAllowance,
  expectedAmount,
  comparison = "gte",
  onRead,
  onSatisfied,
}: WaitForAllowanceOptions) {
  let hasSuccessfulRead = false;
  let lastReadError: unknown;

  for (let attempt = 0; attempt <= allowanceRetryDelays.length; attempt++) {
    let currentAllowance: string;

    try {
      currentAllowance = await readAllowance();
    } catch (error) {
      lastReadError = error;
      currentAllowance = "";
    }

    if (currentAllowance !== "") {
      hasSuccessfulRead = true;
      onRead?.(currentAllowance);

      const current = new Decimal(currentAllowance);
      const expected = new Decimal(expectedAmount);
      const isSatisfied =
        comparison === "eq" ? current.eq(expected) : current.gte(expected);

      if (isSatisfied) {
        await onSatisfied?.(currentAllowance);
        return currentAllowance;
      }
    }

    if (attempt < allowanceRetryDelays.length) {
      await new Promise((resolve) =>
        setTimeout(resolve, allowanceRetryDelays[attempt]),
      );
    }
  }

  if (!hasSuccessfulRead && lastReadError) {
    throw lastReadError;
  }

  throw new SDKError("Insufficient allowance");
}
