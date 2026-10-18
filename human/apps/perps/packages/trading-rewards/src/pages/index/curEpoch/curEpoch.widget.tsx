import { FC } from "react";
import { useCurEpochScript } from "./curEpoch.script";
import { CurEpoch } from "./curEpoch.ui";

export const CurEpochWidget: FC = () => {
  const state = useCurEpochScript();
  return <CurEpoch {...state} />;
};
