import { ElementType } from "react";
import { useModal, modal } from "../modal";
import { SimpleDialog, SimpleDialogProps } from "./simpleDialog";
import { ModalArgs } from "../modal/types";

export function createDialogComponent<P extends Partial<SimpleDialogProps>>(
  Comp: ElementType
) {
  return modal.create((props: P) => {
    const { visible, hide, resolve, reject, onOpenChange } = useModal();

    const {
      title,
      size,
      // @ts-ignore deprecated
      contentClassName,
      // @ts-ignore deprecated
      bodyClassName,
      closable,
      classNames,
      ...rest
    } = props;
    return (
      <SimpleDialog
        open={visible}
        onOpenChange={onOpenChange}
        size={size}
        title={title}
        closable={closable}
        classNames={{
          content: contentClassName,
          body: bodyClassName,
          ...classNames,
        }}
        contentProps={{
          onInteractOutside: (e) => {
            const el = document.querySelector('#privy-dialog')
            if (el) {
              e.preventDefault();
            }
          },
        }}
      >
        <Comp {...rest} close={hide} resolve={resolve} reject={reject} />
      </SimpleDialog>
    );
  });
}

export function registerSimpleDialog<Props = {}>(
  id: string,
  comp: ElementType<Props>,
  props?: Partial<SimpleDialogProps & Props>
) {
  modal.register(id, createDialogComponent(comp), props);
}
