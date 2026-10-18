import { useCallback } from "react";
import { useSubAccountMutation } from "../subAccount/useSubAccountMutation";
import { useCollateral } from "./useCollateral";
import { useMaxWithdrawal } from "./useMaxWithdrawal";

export type TransferReturn = ReturnType<typeof useTransfer>;

type Receiver = {
  account_id: string;
  amount: number;
};

type TransferOptions = {
  /** if not provided, use current account id */
  fromAccountId?: string;
  token?: string;
};

export const useTransfer = (options?: TransferOptions) => {
  const { fromAccountId, token = "" } = options || {};

  const { unsettledPnL, holding } = useCollateral();

  const [doTransfer, { isMutating: submitting }] = useSubAccountMutation(
    "/v1/internal_transfer",
    "POST",
    {
      accountId: fromAccountId,
    },
  );

  const transfer = useCallback(
    async (token: string, receivers: Receiver | Receiver[]) => {
      return doTransfer({
        token,
        receiver_list: Array.isArray(receivers) ? receivers : [receivers],
      })
        .then((res) => {
          if (res.success) {
            return res;
          }
          throw res;
        })
        .catch((err) => {
          throw err;
        });
    },
    [doTransfer],
  );

  const maxAmount = useMaxWithdrawal(token);

  return {
    submitting,
    transfer,
    maxAmount,
    unsettledPnL,
    holding,
  };
};
