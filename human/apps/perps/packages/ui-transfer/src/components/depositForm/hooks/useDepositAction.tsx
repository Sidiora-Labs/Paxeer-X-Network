import { useCallback, useRef, useState } from "react";
import { useEventEmitter } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { getDepositKnownErrorMessage } from "../../../utils";

type Options = {
  quantity: string;
  approve: (quantity?: string) => Promise<any>;
  deposit: () => Promise<any>;
  onSuccess?: () => void;
  swapDeposit?: () => Promise<any>;
  needSwap?: boolean;
};

export function useDepositAction(options: Options) {
  const { quantity, approve, deposit, swapDeposit, onSuccess, needSwap } =
    options;
  const [isMutating, setIsMutating] = useState(false);
  const [depositError, setDepositError] = useState("");
  const mutationLock = useRef(false);

  const ee = useEventEmitter();
  const { t } = useTranslation();

  const runDeposit = useCallback(async () => {
    try {
      if (needSwap) {
        await swapDeposit?.();
      } else {
        await deposit();
      }
      setDepositError("");
    } catch (err: any) {
      console.error("paxeer-network deposit error", err);
      const knownErrorMessage = getDepositKnownErrorMessage(err.message);
      if (knownErrorMessage) {
        setDepositError(knownErrorMessage);
        toast.error(
          <div>
            {t("common.somethingWentWrong")}
            <br />
            <div className="paxeer-network-text-white/[0.54] paxeer-network-text-xs">
              {t("common.details")}: {knownErrorMessage}
            </div>
          </div>,
        );
      } else {
        toast.error(err.message || t("common.somethingWentWrong"));
      }
      throw err;
    }
  }, [deposit, needSwap, swapDeposit, t]);

  const runApprove = useCallback(async () => {
    try {
      await approve(quantity);
      toast.success(t("transfer.deposit.approve.success"));
    } catch (err: any) {
      console.error("approve error", err);
      toast.error(
        err.message || err?.errorCode || t("transfer.deposit.approve.failed"),
      );
      throw err;
    }
  }, [approve, quantity, t]);

  const isValidQuantity = useCallback(() => {
    const num = Number(quantity);

    if (isNaN(num) || num <= 0) {
      toast.error(t("transfer.quantity.invalid"));
      return false;
    }

    return true;
  }, [quantity, t]);

  const startMutation = useCallback(() => {
    if (mutationLock.current) {
      return false;
    }

    mutationLock.current = true;
    setIsMutating(true);
    return true;
  }, []);

  const finishMutation = useCallback(() => {
    mutationLock.current = false;
    setIsMutating(false);
  }, []);

  const onDeposit = useCallback(async () => {
    if (mutationLock.current) return;
    if (!isValidQuantity() || !startMutation()) {
      return;
    }

    try {
      await runDeposit();
      toast.success(t("transfer.deposit.requested"));
      ee.emit("deposit:requested");
      onSuccess?.();
    } catch (err: any) {
      // runDeposit reports the error to the user.
    } finally {
      finishMutation();
    }
  }, [
    ee,
    finishMutation,
    isValidQuantity,
    onSuccess,
    runDeposit,
    startMutation,
    t,
  ]);

  const onApprove = useCallback(async () => {
    if (!startMutation()) return;
    try {
      await runApprove();
    } finally {
      finishMutation();
    }
  }, [finishMutation, runApprove, startMutation]);

  const onApproveAndDeposit = useCallback(async () => {
    if (mutationLock.current) return;
    if (!isValidQuantity() || !startMutation()) return;

    try {
      await runApprove();
      await runDeposit();
      toast.success(t("transfer.deposit.requested"));
      ee.emit("deposit:requested");
      onSuccess?.();
    } catch (err) {
      console.error("approve and deposit error", err);
    } finally {
      finishMutation();
    }
  }, [
    ee,
    finishMutation,
    isValidQuantity,
    onSuccess,
    runApprove,
    runDeposit,
    startMutation,
    t,
  ]);

  return {
    isMutating,
    depositError,
    setDepositError,
    onApprove,
    onDeposit,
    onApproveAndDeposit,
  };
}
