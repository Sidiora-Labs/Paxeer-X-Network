import {
  BuildNode,
  ReferralContextReturns,
  useReferralContext,
} from "../../../provider";

export type TopReturns = {
  state: ReferralContextReturns;
  overwriteTop?: BuildNode;
};

export const useTopScript = (): TopReturns => {
  const state = useReferralContext();

  return {
    overwriteTop: state.overwrite?.ref?.top,
    state,
  };
};
