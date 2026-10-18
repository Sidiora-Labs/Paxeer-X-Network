import { useCallback, useState } from "react";
import { useAccount } from "../useAccount";

export type InternalTransferReturn = ReturnType<typeof useInternalTransfer>;

export type InternalTransferInputs = {
  token: string;
  amount: string;
  receiver: string;
  /** paxeer-network token decimals */
  decimals: number;
};

export const useInternalTransfer = () => {
  const [submitting, setSubmitting] = useState(false);
  const { account } = useAccount();

  const transfer = useCallback(
    async (inputs: InternalTransferInputs) => {
      setSubmitting(true);
      return account.assetsManager
        .internalTransfer(inputs)
        .then((res: any) => {
          if (res.success) {
            return res;
          }
          throw res;
        })
        .catch((err) => {
          throw err;
        })
        .finally(() => {
          setSubmitting(false);
        });
    },
    [account],
  );

  return {
    submitting,
    transfer,
  };
};
