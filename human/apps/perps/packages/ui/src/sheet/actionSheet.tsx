import React, {
  ComponentPropsWithoutRef,
  FC,
  Fragment,
  PropsWithChildren,
  useCallback,
  useMemo,
} from "react";
import { cn, Divider, Sheet, SheetContent, SheetTrigger } from "..";
import { useLocale } from "../locale";

type SheetItemType = "division" | "data" | "cancel";

export interface BaseActionSheetItem {
  label: string;
  type?: SheetItemType;
  icon?: string;
  value?: string;
  active?: boolean;
  onClick?: (action: BaseActionSheetItem) => void;
}

export interface ActionSheetItemProps {
  action: BaseActionSheetItem;
  index: number;
  active?: boolean;
  onClick?: (value: { value?: string; index: number }) => void;
}

export const ActionItem: FC<ActionSheetItemProps> = (props) => {
  const { action } = props;

  const onItemClick = useCallback(() => {
    if (typeof action.onClick === "function") {
      action.onClick(action);
    } else {
      props.onClick?.({ ...action, index: props.index });
    }
  }, [action]);

  const child = useMemo(() => {
    return action.label;
  }, [action.label]);

  return (
    <div
      className={cn(
        "oui-flex oui-justify-center oui-items-center oui-text-lg oui-h-[52px] oui-cursor-pointer",

        props.active && "oui-text-primary-darken",
      )}
      onClick={onItemClick}
    >
      {child}
    </div>
  );
};

export const ActionDivision: FC = (props) => {
  // TODO(theme): "oui-bg-base-300" is an invalid token (the base scale only defines 1-10) and generates no CSS, so this divider is invisible. Remove it or replace with "oui-bg-base-3" after visual comparison.
  return (
    <div className="oui-relative oui-h-[1px] oui-bg-base-300 oui-mt-[-1px]"></div>
  );
};

export interface ActionSheetContentProps {
  actionSheets: BaseActionSheetItem[];
  value?: BaseActionSheetItem;
  onValueChange?: (value: any) => void;
  onClose?: () => void;
}
export const ActionSheetContent: FC<ActionSheetContentProps> = (props) => {
  return (
    <>
      {props.actionSheets.map((action, index) => {
        if (action.type === "division") {
          return <ActionDivision key={index} />;
        }
        return (
          <Fragment key={action.value || index}>
            {action.value === "cancel" && (
              <div className="oui-h-2 oui-bg-base-10" />
            )}
            <ActionItem
              onClick={(value) => {
                if (value.value === "cancel") {
                  return;
                }

                if (typeof action.onClick === "function") {
                  action.onClick(action);
                } else {
                  props.onValueChange?.(value.value);
                  props.onClose?.();
                }
              }}
              index={index}
              action={action}
              active={
                typeof props.value !== "undefined" &&
                props.value.value === action.value
              }
            />
            {index < props.actionSheets.length - 1 && (
              <Divider className="oui-border-base-contrast/10" />
            )}
          </Fragment>
        );
      })}
    </>
  );
};

export type SystemActionSheetItem = "Cancel" | "Confirm" | "---";

export type ActionSheetItem = BaseActionSheetItem | SystemActionSheetItem;

export interface ActionSheetProps extends ComponentPropsWithoutRef<
  typeof Sheet
> {
  actionSheets: ActionSheetItem[];
  value?: BaseActionSheetItem;
  onValueChange?: (value: string) => void;
  open?: boolean;
  onClose?: () => void;
}

export const ActionSheet: FC<PropsWithChildren<ActionSheetProps>> = (props) => {
  const [locale] = useLocale("modal");
  // create actionSheet items
  const items = useMemo<BaseActionSheetItem[]>(() => {
    const items: BaseActionSheetItem[] = [];

    if (Array.isArray(props.actionSheets)) {
      for (const action of props.actionSheets) {
        if (typeof action === "string") {
          if (action === "Cancel") {
            items.push({
              label: locale.cancel,
              value: "cancel",
              onClick: () => {
                props.onClose?.();
              },
            });
          } else if (action === "Confirm") {
            items.push({
              label: locale.confirm,
              value: "confirm",
            });
          } else if (action.startsWith("---")) {
            items.push({
              label: "---",
              value: "---",
              type: "division",
            });
          }
        } else {
          items.push(action);
        }
      }
    }

    return items;
  }, [props.actionSheets]);

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      {typeof props.children !== "undefined" && (
        <SheetTrigger asChild>{props.children}</SheetTrigger>
      )}

      <SheetContent
        closeable={false}
        className="oui-action-sheet-content !oui-p-0 oui-pb-[env(safe-area-inset-bottom)]"
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <ActionSheetContent
          actionSheets={items}
          onClose={props.onClose}
          onValueChange={props.onValueChange}
          value={props.value}
        />
      </SheetContent>
    </Sheet>
  );
};
