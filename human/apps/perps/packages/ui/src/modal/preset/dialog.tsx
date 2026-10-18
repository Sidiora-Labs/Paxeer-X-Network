import { ReactNode } from "react";
import { SimpleDialog, SimpleDialogProps } from "../../dialog/simpleDialog";
import { modalActions } from "../modalContext";
import { create } from "../modalHelper";
import { useModal } from "../useModal";

export type DialogProps = {
  content: ReactNode;
} & Pick<SimpleDialogProps, "title" | "closable" | "size" | "actions">;

const Dialog = create<DialogProps>((props) => {
  const { visible, hide, resolve, reject, onOpenChange } = useModal();
  return (
    <SimpleDialog
      title={props.title}
      open={visible}
      onOpenChange={onOpenChange}
      size={props.size}
      closable={props.closable}
      actions={props.actions}
      // // @ts-ignore
      // onOk={() => {
      //   resolve(true);
      //   hide();
      // }}
      // onCancel={() => {
      //   reject(false);
      //   hide();
      // }}
    >
      {props.content}
    </SimpleDialog>
  );
});

export const dialog = (props: DialogProps) => {
  return modalActions.show(Dialog, props);
};
