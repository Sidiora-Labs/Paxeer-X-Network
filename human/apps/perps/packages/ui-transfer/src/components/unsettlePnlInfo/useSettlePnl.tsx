import { useMemo } from "react";
import {
  useAccount,
  useEventEmitter,
  usePositionStream,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";

export type SettlePnlOptions = {
  accountId?: string;
};

export const useSettlePnl = (options?: SettlePnlOptions) => {
  const { accountId } = options || {};
  const { t } = useTranslation();
  const ee = useEventEmitter();
  const { account, state } = useAccount();
  const [positionData] = usePositionStream();

  const hasPositions = useMemo(
    () => !!positionData?.rows?.length,
    [positionData],
  );

  const onSettlePnl = async () => {
    const isSubAccount = accountId && state.mainAccountId !== accountId;
    const settleFn = isSubAccount
      ? account.settleSubAccount({ subAccountId: accountId })
      : account.settle({ accountId });
    return settleFn
      .then((res) => {
        toast.success(t("settle.settlement.requested"));
        return Promise.resolve(res);
      })
      .catch((e) => {
        if (e.code == -1104) {
          toast.error(t("settle.settlement.error"));
        } else if (
          e.message.indexOf(
            "Signing off chain messages with Ledger is not yet supported",
          ) !== -1
        ) {
          ee.emit("wallet:sign-message-with-ledger-error", {
            message: e.message,
            userAddress: account.address,
          });
        } else if (e.message.indexOf("user rejected") !== -1) {
          toast.error(t("transfer.rejectTransaction"));
        } else {
          toast.error(e.message);
          return Promise.reject(e);
        }
      });
  };

  return {
    hasPositions,
    onSettlePnl,
  };
};
