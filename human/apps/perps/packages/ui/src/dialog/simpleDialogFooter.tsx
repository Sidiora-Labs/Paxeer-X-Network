import { FC, useEffect, useMemo, useState } from "react";
import { Button, ButtonProps, ThrottledButton } from "../button";
import { DialogFooter } from "./dialog";

export type DialogAction<T = any> = {
  label: string;
  onClick: (event: any) => Promise<T> | T;
} & Pick<
  ButtonProps,
  | "size"
  | "disabled"
  | "className"
  | "fullWidth"
  | "data-testid"
  | "loading"
  | "variant"
  | "color"
>;

export type SimpleDialogFooterProps = {
  actions?: {
    primary?: DialogAction;
    secondary?: DialogAction;
  };
  className?: string;
};

export const SimpleDialogFooter: FC<SimpleDialogFooterProps> = (props) => {
  const { actions } = props;
  const [primaryLoading, setPrimaryLoading] = useState(
    actions?.primary?.loading ?? false,
  );

  useEffect(() => {
    if (actions?.primary?.loading) {
      setPrimaryLoading(actions?.primary?.loading);
    }

    return () => {
      setPrimaryLoading(false);
    };
  }, [actions?.primary?.loading]);

  const buttons = useMemo(() => {
    if (!actions) {
      return [];
    }

    const buttons = [];

    if (actions.secondary && typeof actions.secondary.onClick === "function") {
      const {
        fullWidth = true,
        color = "gray",
        label,
        ...rest
      } = actions.secondary;

      buttons.push(
        <Button
          key="secondary"
          {...rest}
          data-testid={actions.secondary?.["data-testid"]}
          color={color}
          fullWidth={fullWidth}
        >
          {label}
        </Button>,
      );
    }

    if (actions.primary && typeof actions.primary.onClick === "function") {
      const {
        fullWidth = true,
        color,
        disabled,
        label,
        onClick,
        ...rest
      } = actions.primary;

      buttons.push(
        <ThrottledButton
          key="primary"
          {...rest}
          data-testid={actions.primary?.["data-testid"]}
          onClick={async (event) => {
            if (primaryLoading) return;
            try {
              setPrimaryLoading(true);
              await onClick(event);
            } catch (e) {
            } finally {
              setPrimaryLoading(false);
            }
          }}
          disabled={disabled || primaryLoading}
          loading={primaryLoading}
          fullWidth={fullWidth}
          color={color}
        >
          {label}
        </ThrottledButton>,
      );
    }

    return buttons;
  }, [actions, primaryLoading]);

  if (!actions) return null;

  return <DialogFooter className={props.className}>{buttons}</DialogFooter>;
};
