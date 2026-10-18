import { FC, useState } from "react";
import { OrderCellState } from "../orderCell.script";
import { useOrderListContext } from "../../orderListContext";
import { toast } from "@paxeer-network/ui";

export const useCancelBtnScript = (props: { state: OrderCellState }) => {
  const { state } = props;
  const [open, setOpen] = useState(false);

  const { onCancelOrder } = useOrderListContext();
  const [isLoading, setIsLoading] = useState(false);

  const onCancel = (event?: any) => {
    if (!onCancelOrder) return Promise.resolve();
    event?.preventDefault();
    event?.stopPropagation();
    setIsLoading(true);
    return onCancelOrder(state.item)
      .then(
        (res: any) => {
          setOpen(false);
        },
        (error: any) => {
          toast.error(error.message);
        }
      ).finally(() => {
        setIsLoading(false);
      });
  };

  const onClose = () => {
    setOpen(false);
  };

  return {
    ...state,

    open,
    setOpen,
    onCancel,
    onClose,
    isLoading,
  };
};

export type CancelBtnState = ReturnType<typeof useCancelBtnScript>;
