import { useMemo } from "react";
import { DepositAction } from "../../../types";

type Options = {
  allowance: string;
  quantity: string;
  maxQuantity: string;
};

export function useActionType(options: Options) {
  const { allowance, quantity, maxQuantity } = options;

  const actionType = useMemo(() => {
    const allowanceNum = Number(allowance);

    if (allowanceNum <= 0) {
      return DepositAction.ApproveAndDeposit;
    }

    const qty = Number(quantity);
    const maxQty = Number(maxQuantity);

    if (allowanceNum < qty && qty <= maxQty) {
      return DepositAction.ApproveAndDeposit;
    }

    return DepositAction.Deposit;
  }, [allowance, quantity, maxQuantity]);

  return actionType;
}
