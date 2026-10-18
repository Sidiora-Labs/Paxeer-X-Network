import { modal } from "@paxeer-network/ui";
import { useReferralContext } from "../../../../provider";
import { ReferralCodeFormType } from "../../../../types";
import { ReferralCodeFormDialogId } from "../referralCodeForm/modal";

export const useMultiLevelReferralScript = () => {
  const {
    isMultiLevelReferralUnlocked,
    maxRebateRate,
    bonusMaxRebateRate,
    baseRebateRate,
    generateCode,
    multiLevelRebateInfoMutate,
  } = useReferralContext();

  const createReferralCode = () => {
    modal.show(ReferralCodeFormDialogId, {
      type: ReferralCodeFormType.Create,
      maxRebateRate,
      bonusMaxRebateRate,
      baseRebateRate,
      referralCode: generateCode?.code,
      // because there is no multi level code, the /v1/referral/multi_level/rebate_info interface will throw an error, so here set to 0, and hide the extra bonus display when creating multi level code
      directBonusRebateRate: 0,
      onSuccess: () => {
        multiLevelRebateInfoMutate();
      },
    });
  };

  return {
    isMultiLevelReferralUnlocked,
    createReferralCode,
  };
};

export type MultiLevelReferralReturns = ReturnType<
  typeof useMultiLevelReferralScript
>;
