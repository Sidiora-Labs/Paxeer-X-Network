import { ReactNode } from "react";
import { modalActions } from "../modalContext";
import { create } from "../modalHelper";
import { useModal } from "../useModal";
import { SimpleSheet, SimpleSheetProps } from "../../sheet/simpleSheet";

export type SheetProps = Pick<
  SimpleSheetProps,
  "title" | "classNames" | "leading" | "contentProps" | "closable"
> & {
  content: ReactNode;
};
const BaseSheet = create<SheetProps>((props) => {
  const { visible, hide, resolve, onOpenChange } = useModal();

  return (
    <SimpleSheet open={visible} onOpenChange={onOpenChange} {...props}>
      {props.content}
    </SimpleSheet>
  );
});

export const sheet = (props: SheetProps) => {
  return modalActions.show(BaseSheet, props);
};
