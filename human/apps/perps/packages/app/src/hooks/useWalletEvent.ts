import { useRef } from "react";
import {
  useEventEmitter,
  useSessionStorage,
  useWalletSubscription,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { capitalizeString } from "@paxeer-network/utils";

export function useWalletEvent() {
  const { t } = useTranslation();
  const ee = useEventEmitter();

  const recordRef = useRef<Record<number, boolean>>({});

  const [record, setRecord] = useSessionStorage(
    "paxeerNetwork_wallet_change_id",
    {} as Record<number, boolean>,
  );

  recordRef.current = record;

  useWalletSubscription({
    onMessage: (data: any) => {
      console.log("wallet:changed", data);
      const { id, side, transStatus } = data;
      let showToast = true;

      // DEPOSIT and WITHDRAW will push twice COMPLETED and FAILED event
      if (
        ["DEPOSIT", "WITHDRAW"].includes(side) &&
        ["COMPLETED", "FAILED"].includes(transStatus)
      ) {
        const isPushed = !!recordRef.current[id];
        if (!isPushed) {
          recordRef.current[id] = true;
          setRecord((prev: Record<number, boolean>) => ({
            ...prev,
            [id]: true,
          }));
        }
        showToast = !isPushed;
      }

      if (transStatus === "COMPLETED" && showToast) {
        let msg = `${capitalizeString(side)} completed`;

        if (side === "DEPOSIT") {
          msg = t("transfer.deposit.completed");
        } else if (side === "WITHDRAW") {
          msg = t("transfer.withdraw.completed");
        }

        toast.success(msg);
      } else if (transStatus === "FAILED" && showToast) {
        let msg = `${capitalizeString(side)} failed`;

        if (side === "DEPOSIT") {
          msg = t("transfer.deposit.failed");
        } else if (side === "WITHDRAW") {
          msg = t("transfer.withdraw.failed");
        }
        toast.error(msg);
      }

      ee.emit("wallet:changed", data);
    },
  });
}
