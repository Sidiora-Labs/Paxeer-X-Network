import { useMutation } from "@paxeer-network/hooks";

export const useReferralCode = () => {
  const [doBindReferralCode, { isMutating: isBindMutating }] =
    useMutation("/v1/referral/bind");

  const [doCreateReferralCode, { isMutating: isCreateMutating }] = useMutation(
    "/v1/referral/multi_level/claim_code",
  );

  const [doEditReferralCode, { isMutating: isEditMutating }] = useMutation(
    "/v1/referral/edit_referral_code",
  );

  const [doUpdateRebateRate, { isMutating: isUpdateRebateRateMutating }] =
    useMutation("/v1/referral/multi_level/rebate_rate/update");

  const [doResetRebateRate, { isMutating: isResetRebateRateMutating }] =
    useMutation("/v1/referral/multi_level/rebate_rate/set_default");

  const [doUpdateKickbackRatio, { isMutating: isUpdateKickbackRatioMutating }] =
    useMutation("/v1/referral/multi_level/kickback_ratio/update");

  const createReferralCode = async (params: {
    referral_code: string;
    referee_rebate_rate: number;
  }) => {
    return doCreateReferralCode(params);
  };

  const bindReferralCode = async (params: { referral_code: string }) => {
    return doBindReferralCode(params);
  };

  const editReferralCode = async (params: {
    current_referral_code: string;
    new_referral_code: string;
  }) => {
    return doEditReferralCode(params);
  };

  const updateRebateRate = async (params: {
    referee_rebate_rate: number;
    account_ids?: string[];
  }) => {
    return doUpdateRebateRate(params);
  };

  const updateKickbackRatio = async (params: {
    referee_kickback_ratio: number;
    account_ids?: string[];
  }) => {
    return doUpdateKickbackRatio(params);
  };

  const resetRebateRate = async (params: { account_ids: string[] }) => {
    return doResetRebateRate(params);
  };

  const isMutating =
    isBindMutating ||
    isCreateMutating ||
    isEditMutating ||
    isUpdateRebateRateMutating ||
    isResetRebateRateMutating ||
    isUpdateKickbackRatioMutating;

  return {
    bindReferralCode,
    createReferralCode,
    editReferralCode,
    updateRebateRate,
    updateKickbackRatio,
    resetRebateRate,
    isMutating,
  } as const;
};
