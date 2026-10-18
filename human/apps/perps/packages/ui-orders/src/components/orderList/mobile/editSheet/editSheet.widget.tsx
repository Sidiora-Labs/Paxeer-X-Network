import { API, OrderEntity } from "@paxeer-network/types";
import { OrderCellState } from "../orderCell.script";
import { useEditSheetScript } from "./editSheet.script";
import { EditSheet } from "./editSheet.ui";

type EditSheetWidgetProps = {
  state: OrderCellState;
  editAlgoOrder: (id: string, order: OrderEntity) => Promise<any>;
  editOrder: (id: string, order: OrderEntity) => Promise<any>;
  position?: API.PositionTPSLExt;
  onClose: () => void;
};

export const EditSheetWidget = (props: EditSheetWidgetProps) => {
  const state = useEditSheetScript(props);
  return <EditSheet {...state} />;
};
