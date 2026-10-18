import { TPSLEditModal } from "@paxeer-network/ui-tpsl";
import { TabType } from "../../../orders.widget";
import { EditSheetWidget } from "../editSheet";
import { OrderCellState } from "../orderCell.script";
import { useEditBtnScript } from "./editBtn.script";
import { EditBtn, EditOrderSheet } from "./editBtn.ui";

export const EditBtnWidget = (props: { state: OrderCellState }) => {
  const state = useEditBtnScript(props);
  return (
    <>
      <EditBtn onShowEditSheet={state.onShowEditSheet} />
      {state.type === TabType.tp_sl && state.tpslSheetMounted && (
        <TPSLEditModal
          mode="sheet"
          open={state.tpslSheetOpen}
          onOpenChange={state.setTPSLSheetOpen}
          order={props.state.item}
          position={state.position}
        />
      )}
      {state.type !== TabType.tp_sl && state.sheetMounted && (
        <EditOrderSheet
          open={state.sheetOpen}
          onOpenChange={state.setSheetOpen}
        >
          <EditSheetWidget
            state={props.state}
            position={state.position}
            editAlgoOrder={state.editAlgoOrder}
            editOrder={state.editOrder}
            onClose={state.onCloseEditSheet}
          />
        </EditOrderSheet>
      )}
    </>
  );
};
