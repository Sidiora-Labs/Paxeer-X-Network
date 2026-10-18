import { FC } from "react";
import { OrderCellState } from "../orderCell.script";
import { useCancelBtnScript } from "./cancelBtn.script";
import { CancelBtn } from "./cancelBtn.ui";

export const CancelBtnWidget = (props: { state: OrderCellState }) => {
  const state = useCancelBtnScript(props);
  return <CancelBtn {...state} />;
};
