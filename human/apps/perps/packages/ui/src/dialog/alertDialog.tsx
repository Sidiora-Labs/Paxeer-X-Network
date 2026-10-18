import { FC, ReactNode, useMemo } from "react";
import { SimpleDialog, SimpleDialogProps } from "./simpleDialog";
import { DialogAction } from "./simpleDialogFooter";
import { useScreen } from "../hooks";
import { cnBase } from "tailwind-variants";
import { useLocale } from "../locale";

export type AlertDialogProps = {
  message?: ReactNode;
  onOk?: () => Promise<boolean>;
  onCancel?: () => void;
  okLabel?: string;
  cancelLabel?: string;
  actions?: {
    primary?: Partial<DialogAction>;
    secondary?: Partial<DialogAction>;
  };
} & Pick<
  SimpleDialogProps,
  "open" | "onOpenChange" | "title" | "closable" | "classNames" | "size"
>;

/**
 * Generic alert dialog, often used for confirmation/alert/information dialogs.
 */
export const AlertDialog: FC<AlertDialogProps> = (props) => {
  const [locale] = useLocale("dialog");
  const {
    title,
    message,
    open,
    onOpenChange,
    onOk,
    onCancel,
    okLabel = locale.ok,
    cancelLabel = locale.cancel,
    size,
    classNames,
  } = props;

  const { isMobile } = useScreen();

  const actions = useMemo(() => {
    if (typeof onOk !== "function" && typeof onCancel !== "function")
      return undefined;
    const actions: any = {};
    if (typeof onCancel === "function") {
      actions["secondary"] = {
        label: cancelLabel,
        onClick: onCancel,
        size: "md",
        fullWidth: true,
        ...props.actions?.secondary,
      } as DialogAction;
    }

    if (typeof onOk === "function") {
      actions["primary"] = {
        label: okLabel,
        size: "md",
        fullWidth: true,
        className: "oui-w-full lg:oui-w-[154px]",
        onClick: onOk,
        ...props.actions?.primary,
      } as DialogAction;
    }

    return actions;
  }, [onOk, onCancel, okLabel, cancelLabel, props.actions]);

  const defaultSize = isMobile ? "xs" : "sm";

  return (
    <SimpleDialog
      open={open}
      title={title}
      size={size || defaultSize}
      actions={actions}
      onOpenChange={onOpenChange}
      classNames={{
        content: cnBase(
          "oui-bg-base-8 oui-font-semibold oui-border oui-border-line-6",
          "oui-p-4 oui-pt-0 lg:oui-p-5 lg:oui-pt-0",
          classNames?.content
        ),
        body: cnBase("oui-py-4 lg:oui-py-5", classNames?.body),
        footer: cnBase(
          "oui-justify-center oui-pb-0 oui-pt-2 lg:oui-pt-3",
          classNames?.footer
        ),
      }}
    >
      {message}
    </SimpleDialog>
  );
};
