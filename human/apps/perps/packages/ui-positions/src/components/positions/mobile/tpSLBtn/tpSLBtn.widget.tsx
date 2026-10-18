import { PositionCellState } from "../positionCell/positionCell.script";
import { useTpSLBtnScript } from "./tpSLBtn.script";
import { TpSLBtn } from "./tpSLBtn.ui";

export const TpSLBtnWidget = (props: { state: PositionCellState }) => {
    const state = useTpSLBtnScript(props);
    return (<TpSLBtn {...state} />);
};
