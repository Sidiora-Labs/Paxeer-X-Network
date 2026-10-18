import { AlgoOrder, API } from "@paxeer-network/types";
import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import { TPSLEditModal } from "../positionTPSL";
import { useTPSLDetail } from "./tpslDetail.script";
import { TPSLDetailProvider } from "./tpslDetailProvider";
import { TPSLDetailUI } from "./tsplDetail.ui";

export type TPSLDetailProps = {
  position: API.Position;
  order: AlgoOrder;
  baseDP: number;
  quoteDP: number;
};

export const TPSLDetailWidget = (props: TPSLDetailProps) => {
  const state = useTPSLDetail(props);
  return (
    <>
      <TPSLDetailProvider
        symbol={props.position.symbol}
        position={props.position}
      >
        <TPSLDetailUI {...state} />
      </TPSLDetailProvider>
      {state.tpslEditOrder && (
        <TPSLEditModal
          mode={state.tpslEditMode}
          open={state.tpslEditOpen}
          onOpenChange={state.onTPSLEditOpenChange}
          order={state.tpslEditOrder}
          position={state.position}
        />
      )}
    </>
  );
};
export const TPSLDetailDialogId = "TPSLDetailDialogId ";
export const TPSLDetailSheetId = "TPSLDetailSheetId";
registerSimpleDialog(TPSLDetailDialogId, TPSLDetailWidget, {
  classNames: {
    content: "oui-w-[420px] lg:oui-pt-4 lg:oui-pb-2 !oui-px-0",
    body: "lg:oui-py-0",
  },
});

registerSimpleSheet(TPSLDetailSheetId, TPSLDetailWidget, {
  classNames: {
    content: "!oui-p-0",
  },
});
