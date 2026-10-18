import { useState } from "react";
import { PaxeerNetworkOrder, PositionType } from "@paxeer-network/types";

type OrderValueKeys = keyof PaxeerNetworkOrder;
export type PositionTypeProps = {
  disableSelector?: boolean;
  value: PositionType;
  onChange: (key: OrderValueKeys, value: any) => void;
};

export const useTPSLPositionTypeScript = (props: PositionTypeProps) => {
  return {
    value: props.value,
    onChange: props.onChange,
    disableSelector: props.disableSelector,
  };
};
