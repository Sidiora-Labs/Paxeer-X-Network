import React from "react";
import type { PositionsProps } from "../../types/types";
import { useCombinePositionsScript } from "./combinePositions.script";
import { usePositionsScript } from "./positions.script";
import { MobilePositions, Positions, CombinePositions } from "./positions.ui";

export const PositionsWidget: React.FC<PositionsProps> = (props) => {
  const state = usePositionsScript(props);
  return <Positions {...state} />;
};

export const MobilePositionsWidget: React.FC<PositionsProps> = (props) => {
  const state = usePositionsScript(props);
  return <MobilePositions {...state} />;
};

export const CombinePositionsWidget: React.FC<PositionsProps> = (props) => {
  const state = useCombinePositionsScript(props);
  return <CombinePositions {...state} />;
};
