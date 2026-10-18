import { FC, PropsWithChildren, ReactNode } from "react";
import * as SheetPrimitive from "@radix-ui/react-dialog";
import { Sheet, SheetContent, SheetHeader } from ".";
import { Divider } from "../divider";
import { SheetBody, SheetContentProps, SheetTitle } from "./sheet";

export interface SimpleSheetProps {
  /**
   * Visible header title. When omitted, a screen-reader-only default is still
   * rendered so Radix `Dialog.Content` meets accessibility requirements.
   */
  title?: ReactNode | (() => ReactNode);
  leading?: React.ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  classNames?: {
    content?: string;
    body?: string;
    header?: string;
    overlay?: string;
  };
  contentProps?: SheetContentProps;
  closable?: boolean;
}

export const SimpleSheet: FC<PropsWithChildren<SimpleSheetProps>> = (props) => {
  const {
    open,
    onOpenChange,
    classNames,
    contentProps,
    closable = true,
  } = props;

  const title = typeof props.title === "function" ? props.title() : props.title;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        onOpenAutoFocus={(event) => event.preventDefault()}
        className={classNames?.content}
        overlayClassName={classNames?.overlay}
        closeable={closable}
        {...contentProps}
      >
        {title ? (
          <>
            <SheetHeader leading={props.leading}>
              <SheetTitle>{title}</SheetTitle>
            </SheetHeader>
            <Divider />
          </>
        ) : (
          <SheetPrimitive.Title className="oui-sr-only">
            sheet title
          </SheetPrimitive.Title>
        )}
        <SheetBody className={classNames?.body}>{props.children}</SheetBody>
      </SheetContent>
    </Sheet>
  );
};
