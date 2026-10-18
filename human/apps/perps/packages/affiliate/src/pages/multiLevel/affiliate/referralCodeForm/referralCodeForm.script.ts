import { useMemo, useState } from "react";
import {
  formatReferralCodeInput,
  isReferralCodeLengthValid,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useReferralCode } from "../../../../hooks/useReferralCode";
import { ReferralCodeFormType } from "../../../../types";
import { ReferralCodeFormWidgetProps } from "./referralCodeForm.widget";

export const useReferralCodeFormScript = (
  options: ReferralCodeFormWidgetProps,
) => {
  const { type, referralCode, referrerRebateRate, accountId } = options;
  const { t } = useTranslation();

  const [newCode, setNewCode] = useState<string>(referralCode || "");
  const [isReview, setIsReview] = useState(false);

  const formattedNewCode = useMemo(
    () => formatReferralCodeInput(newCode),
    [newCode],
  );

  const isCodeValid = useMemo(
    () => isReferralCodeLengthValid(formattedNewCode),
    [formattedNewCode],
  );

  const bonusMaxRebatePercentage = useMemo(() => {
    return new Decimal(options.bonusMaxRebateRate ?? options.maxRebateRate ?? 0)
      .mul(100)
      .toNumber();
  }, [options.bonusMaxRebateRate, options.maxRebateRate]);

  const baseRebatePercentage = useMemo(() => {
    return new Decimal(options.baseRebateRate ?? 0).mul(100).toNumber();
  }, [options.baseRebateRate]);

  const totalSplitPercentage = useMemo(() => {
    return new Decimal(bonusMaxRebatePercentage)
      .add(baseRebatePercentage)
      .toNumber();
  }, [bonusMaxRebatePercentage, baseRebatePercentage]);

  const [referrerRebatePercentage, setReferrerRebatePercentage] = useState(
    () => {
      if (type === ReferralCodeFormType.Create) {
        return bonusMaxRebatePercentage;
      }
      if (referrerRebateRate) {
        return new Decimal(referrerRebateRate).mul(100).toNumber();
      }
      return 0;
    },
  );

  const {
    createReferralCode,
    editReferralCode,
    updateRebateRate,
    resetRebateRate,
    isMutating,
  } = useReferralCode();

  const refereeBonusRebatePercentage = useMemo(() => {
    return Math.max(
      0,
      new Decimal(bonusMaxRebatePercentage)
        .sub(referrerRebatePercentage)
        .toNumber(),
    );
  }, [bonusMaxRebatePercentage, referrerRebatePercentage]);

  const refereeRebatePercentage = useMemo(() => {
    return new Decimal(baseRebatePercentage)
      .add(refereeBonusRebatePercentage)
      .toNumber();
  }, [baseRebatePercentage, refereeBonusRebatePercentage]);

  const directTradesPercentage = totalSplitPercentage;

  const indirectTradesPercentage = referrerRebatePercentage;

  const codeChanged = useMemo(() => {
    return newCode !== referralCode;
  }, [newCode, referralCode]);

  const rateChanged = useMemo(() => {
    return (
      new Decimal(referrerRebatePercentage).toNumber() !==
      new Decimal(referrerRebateRate || 0).mul(100).toNumber()
    );
  }, [referrerRebatePercentage, referrerRebateRate, newCode]);

  const getErrorMessage = (err: unknown): string | undefined => {
    if (typeof err === "object" && err !== null && "message" in err) {
      const msg = (err as { message?: unknown }).message;
      return typeof msg === "string" ? msg : undefined;
    }
    return undefined;
  };

  const handleError = (err: unknown) => {
    console.error("handleError", err);
    toast.error(getErrorMessage(err) || t("common.somethingWentWrong"));
  };

  const handleResult = (res: { success?: boolean; message?: string }) => {
    if (res.success) {
      options.onSuccess?.();
      toast.success(t("affiliate.confirmChanges.success"));
      options.close?.();
    } else {
      toast.error(res.message || t("common.somethingWentWrong"));
    }
  };

  const onEdit = async () => {
    if (codeChanged && !isReferralCodeLengthValid(formattedNewCode)) {
      toast.error(t("affiliate.referralCode.editCodeModal.helpText.length"));
      return;
    }

    const editReferralCodeParams = {
      current_referral_code: referralCode!,
      new_referral_code: formattedNewCode,
    };

    const updateRebateRateParams = {
      referee_rebate_rate: new Decimal(refereeBonusRebatePercentage)
        .div(100)
        .toNumber(),
      account_ids: accountId ? [accountId] : undefined,
    };

    try {
      if (codeChanged && rateChanged) {
        const codeRes = await editReferralCode(editReferralCodeParams);
        if (!codeRes.success) {
          toast.error(codeRes.message);
          return;
        }
        const rateRes = await updateRebateRate(updateRebateRateParams);
        handleResult(rateRes);
      } else if (codeChanged) {
        const res = await editReferralCode(editReferralCodeParams);
        handleResult(res);
      } else if (rateChanged) {
        const res = await updateRebateRate(updateRebateRateParams);
        handleResult(res);
      }
    } catch (err) {
      handleError(err);
    }
  };

  const onCreate = async () => {
    if (!isCodeValid) {
      toast.error(t("affiliate.referralCode.editCodeModal.helpText.length"));
      return;
    }

    try {
      const res = await createReferralCode({
        referral_code: formattedNewCode,
        referee_rebate_rate: new Decimal(refereeBonusRebatePercentage)
          .div(100)
          .toNumber(),
      });
      if (res.success) {
        toast.success(t("affiliate.referralCode.create.success"));
        options.onSuccess?.();
        options.close?.();
      } else {
        toast.error(res.message);
      }
    } catch (err) {
      handleError(err);
    }
  };

  const onReset = async () => {
    try {
      const res = await resetRebateRate({ account_ids: [accountId!] });
      handleResult(res);
    } catch (err) {
      handleError(err);
    }
  };

  const onClick = () => {
    switch (type) {
      case ReferralCodeFormType.Create:
        if (!isCodeValid) {
          toast.error(
            t("affiliate.referralCode.editCodeModal.helpText.length"),
          );
          return;
        }
        if (isReview) {
          onCreate();
        } else {
          setIsReview(true);
        }
        break;
      case ReferralCodeFormType.Edit:
        if (accountId || isReview) {
          onEdit();
        } else {
          setIsReview(true);
        }
        break;
      case ReferralCodeFormType.Reset:
        onReset();
        break;
    }
  };

  const buttonDisabled = useMemo(() => {
    if (type === ReferralCodeFormType.Create) {
      return !isCodeValid;
    }
    if (type !== ReferralCodeFormType.Edit) {
      return false;
    }
    if (codeChanged && !isReferralCodeLengthValid(formattedNewCode)) {
      return true;
    }
    return !codeChanged && !rateChanged;
  }, [codeChanged, rateChanged, formattedNewCode, isCodeValid, type]);

  const confirmButtonLoading = isMutating;

  return {
    type,
    onClick,
    maxRebatePercentage: bonusMaxRebatePercentage,
    baseRebatePercentage,
    totalSplitPercentage,
    directTradesPercentage,
    indirectTradesPercentage,
    referrerRebatePercentage,
    setReferrerRebatePercentage,
    refereeRebatePercentage,
    refereeBonusRebatePercentage,
    confirmButtonLoading,
    newCode,
    setNewCode,
    isReview,
    buttonDisabled,
    onReset,
  };
};

export type ReferralCodeFormReturns = ReturnType<
  typeof useReferralCodeFormScript
>;
