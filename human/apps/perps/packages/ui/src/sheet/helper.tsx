import { ElementType } from "react";
import { useModal, modal } from "../modal";
import { SheetProps } from "../modal/preset/sheet";
import { SimpleSheet, SimpleSheetProps } from "./simpleSheet";

function createSheetComponent<P extends Partial<SimpleSheetProps>>(
  Comp: ElementType
) {
  return modal.create((props: P) => {
    const { title, leading, classNames, contentProps, closable, ...rest } =
      props;

    const { visible, hide, resolve, reject, onOpenChange, args } = useModal();

    // console.log("-------", args);

    return (
      <SimpleSheet
        open={visible}
        onOpenChange={onOpenChange}
        title={title}
        leading={leading}
        classNames={classNames}
        closable={closable}
        contentProps={contentProps}
      >
        <Comp {...rest} close={hide} resolve={resolve} reject={reject} />
      </SimpleSheet>
    );
  });
}

export function registerSimpleSheet<Props = {}>(
  id: string,
  comp: ElementType<Props>,
  props?: Partial<SimpleSheetProps & Props>
) {
  modal.register(id, createSheetComponent(comp), props);
}
