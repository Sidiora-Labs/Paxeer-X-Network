import { useCallback } from "react";
import { AlgoOrderRootType, API, PositionType } from "@paxeer-network/types";
import { SimpleDialog, SimpleSheet } from "@paxeer-network/ui";
import { TPSLWidget } from "./tpsl.widget";

export type TPSLEditModalProps = {
  mode: "dialog" | "sheet";
  open: boolean;
  onOpenChange: (open: boolean) => void;
  order: API.AlgoOrder;
  position?: API.Position;
};

export const TPSLEditModal = (props: TPSLEditModalProps) => {
  const { mode, open, onOpenChange, order, position } = props;

  const close = useCallback(() => {
    onOpenChange(false);
  }, [onOpenChange]);

  const content = open ? (
    <TPSLWidget
      symbol={order.symbol}
      order={order}
      position={position}
      positionType={
        order.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
          ? PositionType.FULL
          : PositionType.PARTIAL
      }
      isEditing
      close={close}
    />
  ) : null;

  if (mode === "sheet") {
    return (
      <SimpleSheet open={open} onOpenChange={onOpenChange}>
        {content}
      </SimpleSheet>
    );
  }

  return (
    <SimpleDialog
      open={open}
      onOpenChange={onOpenChange}
      classNames={{ content: "oui-w-[420px]" }}
      contentProps={{
        onInteractOutside: (event) => {
          if (document.querySelector("#privy-dialog")) {
            event.preventDefault();
          }
        },
      }}
    >
      {content}
    </SimpleDialog>
  );
};
