import { FC, PropsWithChildren, ReactNode } from "react";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./dialog";
import { Divider } from "../divider";
import {
  SimpleDialogFooter,
  SimpleDialogFooterProps,
} from "./simpleDialogFooter";

type TriggerDialogProps = {
  size?: "sm" | "md" | "lg";
  closable?: boolean;
  title?: ReactNode;
  description?: ReactNode;
  trigger: ReactNode;
} & SimpleDialogFooterProps;

const TriggerDialog: FC<PropsWithChildren<TriggerDialogProps>> = (props) => {
  return (
    <Dialog>
      <DialogTrigger asChild>{props.trigger}</DialogTrigger>
      <DialogContent
        size={props.size}
        closable={props.closable}
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        {props.title && (
          <>
            <DialogHeader>
              <DialogTitle>{props.title}</DialogTitle>
            </DialogHeader>
            <Divider />
          </>
        )}
        <DialogBody>{props.children}</DialogBody>
        {typeof props.description !== "undefined" && (
          <DialogDescription>{props.description}</DialogDescription>
        )}
        <SimpleDialogFooter actions={props.actions} />
      </DialogContent>
    </Dialog>
  );
};

export { TriggerDialog };

export type { TriggerDialogProps };
