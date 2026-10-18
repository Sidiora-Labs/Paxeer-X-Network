import { useCallback } from "react";
import { AlertDialog, AlertDialogProps } from "../../dialog/alertDialog";
import { modalActions } from "../modalContext";
import { create } from "../modalHelper";
import { useModal } from "../useModal";

export const CreatedAlertDialog = create<AlertDialogProps>((props) => {
  const { onOk } = props;
  const { visible, hide, resolve, reject, onOpenChange } = useModal();

  const onOkHandler = useCallback((): Promise<any> => {
    return Promise.resolve().then(onOk).then(hide);
  }, [onOk]);

  return (
    <AlertDialog
      open={visible}
      onOpenChange={onOpenChange}
      {...props}
      onOk={onOkHandler}
    />
  );
});

export const alert = (props: AlertDialogProps) => {
  return modalActions.show(CreatedAlertDialog, props);
};
