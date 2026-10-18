import {
  BuildNode,
  ReferralContextReturns,
  useReferralContext,
} from "../../../provider";

export type BecomeAffiliateReturns = {
  state: ReferralContextReturns;
  overwrite?: BuildNode;
};

export const useBecomeAffiliateScript = (): BecomeAffiliateReturns => {
  const state = useReferralContext();

  return {
    overwrite: state.overwrite?.ref?.step,
    state,
  };
};
