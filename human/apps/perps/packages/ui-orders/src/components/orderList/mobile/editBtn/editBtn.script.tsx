import { useCallback, useState } from "react";
import { TabType } from "../../../orders.widget";
import { useOrderListContext } from "../../orderListContext";
import { useTPSLOrderRowContext } from "../../tpslOrderRowContext";
import { OrderCellState } from "../orderCell.script";

export const useEditBtnScript = (props: { state: OrderCellState }) => {
  const { state } = props;
  const [tpslSheetMounted, setTPSLSheetMounted] = useState(false);
  const [tpslSheetOpen, setTPSLSheetOpen] = useState(false);
  const [sheetMounted, setSheetMounted] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(false);

  const { editAlgoOrder, editOrder } = useOrderListContext();
  const { position } = useTPSLOrderRowContext();

  const onShowEditSheet = useCallback(() => {
    if (props.state.type === TabType.tp_sl) {
      setTPSLSheetMounted(true);
      setTPSLSheetOpen(true);
    } else {
      setSheetMounted(true);
      setSheetOpen(true);
    }
  }, [props.state.type]);

  const onCloseEditSheet = useCallback(() => {
    setSheetOpen(false);
  }, []);

  return {
    ...state,
    onShowEditSheet,
    tpslSheetMounted,
    tpslSheetOpen,
    setTPSLSheetOpen,
    sheetMounted,
    sheetOpen,
    setSheetOpen,
    onCloseEditSheet,
    position,
    editAlgoOrder,
    editOrder,
  };
};

export type EditBtnState = ReturnType<typeof useEditBtnScript>;
