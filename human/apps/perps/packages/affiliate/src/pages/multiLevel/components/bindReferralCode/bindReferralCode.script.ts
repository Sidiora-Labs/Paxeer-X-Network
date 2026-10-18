import { useMemo, useState } from "react";
import {
  formatReferralCodeInput,
  isReferralCodeLengthValid,
  REFERRAL_CODE_MIN_LENGTH,
  useCheckReferralCode,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { useReferralCode } from "../../../../hooks/useReferralCode";
import { BindReferralCodeWidgetProps } from "./bindReferralCode.widget";

export const useBindReferralCodeScript = (
  options: BindReferralCodeWidgetProps,
) => {
  const { t } = useTranslation();

  const [bindCodeInput, setBindCodeInput] = useState("");
  const [skipBinding, setSkipBinding] = useState(false);
  const [isAwaitingPostSuccess, setIsAwaitingPostSuccess] = useState(false);

  const formattedBindCode = useMemo(
    () => formatReferralCodeInput(bindCodeInput),
    [bindCodeInput],
  );

  const { isExist: isBindCodeExist, isLoading: isBindCodeChecking } =
    useCheckReferralCode(
      formattedBindCode.length >= REFERRAL_CODE_MIN_LENGTH
        ? formattedBindCode
        : undefined,
    );

  const { bindReferralCode, isMutating } = useReferralCode();

  const getErrorMessage = (err: unknown): string | undefined => {
    if (typeof err === "object" && err !== null && "message" in err) {
      const msg = (err as { message?: unknown }).message;
      return typeof msg === "string" ? msg : undefined;
    }
    return undefined;
  };

  const handleError = (err: unknown) => {
    toast.error(getErrorMessage(err) || t("common.somethingWentWrong"));
  };

  const runAfterSuccess = async (payload: { skipped: boolean }) => {
    setIsAwaitingPostSuccess(true);
    try {
      await Promise.resolve(options.onSuccess?.(payload));
    } finally {
      setIsAwaitingPostSuccess(false);
    }
    options.close?.();
  };

  const onConfirm = async () => {
    if (skipBinding) {
      await runAfterSuccess({ skipped: true });
      return;
    }

    if (
      !isReferralCodeLengthValid(formattedBindCode) ||
      isBindCodeChecking ||
      !isBindCodeExist
    ) {
      return;
    }

    try {
      await bindReferralCode({ referral_code: formattedBindCode });
      toast.success(t("affiliate.referralCode.bound"));
      await runAfterSuccess({ skipped: false });
    } catch (err) {
      handleError(err);
    }
  };

  const buttonDisabled =
    !skipBinding &&
    (!isReferralCodeLengthValid(formattedBindCode) ||
      isBindCodeChecking ||
      !isBindCodeExist);

  const confirmButtonLoading = isMutating || isAwaitingPostSuccess;

  return {
    bindCodeInput,
    setBindCodeInput,
    skipBinding,
    setSkipBinding,
    formattedBindCode,
    isBindCodeExist,
    isBindCodeChecking,
    buttonDisabled,
    confirmButtonLoading,
    onConfirm,
  };
};

export type BindReferralCodeScriptReturns = ReturnType<
  typeof useBindReferralCodeScript
>;
