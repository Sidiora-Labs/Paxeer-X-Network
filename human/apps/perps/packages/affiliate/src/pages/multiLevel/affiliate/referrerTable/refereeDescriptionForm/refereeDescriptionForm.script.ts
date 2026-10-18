import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { useEditRefereeDescription } from "../../../../../hooks/useEditRefereeDescription";
import { RefereeDescriptionFormWidgetProps } from "./refereeDescriptionForm.widget";

const DESCRIPTION_PATTERN = /^[a-zA-Z0-9@, _-]{0,50}$/;

export const useRefereeDescriptionFormScript = (
  options: RefereeDescriptionFormWidgetProps,
) => {
  const { t } = useTranslation();
  const [description, setDescription] = useState(options.description ?? "");
  const [isAwaitingPostSuccess, setIsAwaitingPostSuccess] = useState(false);

  const { editRefereeDescription, isMutating } = useEditRefereeDescription();

  const isValid = useMemo(() => {
    return DESCRIPTION_PATTERN.test(description);
  }, [description]);

  const buttonDisabled = !isValid;

  const getErrorMessage = (err: unknown): string | undefined => {
    if (typeof err === "object" && err !== null && "message" in err) {
      const msg = (err as { message?: unknown }).message;
      return typeof msg === "string" ? msg : undefined;
    }
    return undefined;
  };

  const onConfirm = async () => {
    if (!isValid) return;

    const nextDescription = description.length === 0 ? null : description;

    try {
      const res = await editRefereeDescription({
        user_address: options.address,
        description: nextDescription,
      });

      if (res.success) {
        setIsAwaitingPostSuccess(true);
        try {
          await Promise.resolve(options.onSuccess?.());
        } finally {
          setIsAwaitingPostSuccess(false);
        }
        toast.success(t("affiliate.refereeNote.save.success"));
        options.close?.();
      } else {
        toast.error(res.message || t("common.somethingWentWrong"));
      }
    } catch (err) {
      toast.error(getErrorMessage(err) || t("common.somethingWentWrong"));
    }
  };

  return {
    description,
    setDescription,
    isValid,
    buttonDisabled,
    confirmButtonLoading: isMutating || isAwaitingPostSuccess,
    onConfirm,
  };
};

export type RefereeDescriptionFormReturns = ReturnType<
  typeof useRefereeDescriptionFormScript
>;
