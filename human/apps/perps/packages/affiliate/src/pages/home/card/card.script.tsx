import {
  BuildNode,
  ReferralContextReturns,
  useReferralContext,
} from "../../../provider";

export type CardReturns = {
  state: ReferralContextReturns;
  overwrite?: BuildNode;
};

export const useCardScript = (): CardReturns => {
  const state = useReferralContext();

  return {
    overwrite: state.overwrite?.ref?.card,
    state,
  };
};
