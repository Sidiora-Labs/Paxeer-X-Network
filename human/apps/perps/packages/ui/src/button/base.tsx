import React, { useMemo } from "react";
import { Slot } from "@radix-ui/react-slot";
import { Flex } from "../flex";
import {
  ComponentPropsWithout,
  RemovedProps,
} from "../helpers/component-props";
import { SizeType } from "../helpers/sizeType";
import { Spinner } from "../spinner/spinner";

type BaseButtonElement = React.ElementRef<"button">;

export interface BaseButtonProps extends ComponentPropsWithout<
  "button",
  RemovedProps
> {
  loading?: boolean;
  leading?: React.ReactNode;
  trailing?: React.ReactNode;
  asChild?: boolean;
  size: SizeType;
  icon?: React.ReactElement;
  as?: "button" | "a";
}

export const BaseButton = React.forwardRef<BaseButtonElement, BaseButtonProps>(
  (props, forwardedRef) => {
    const {
      asChild = false,
      children,
      loading,
      leading,
      trailing,
      size,
      icon,
      disabled,
      ...rest
    } = props;
    const Comp = asChild ? Slot : "button";

    const isDisabled = typeof disabled !== "undefined" ? disabled : loading;

    const iconElement = useMemo(() => {
      return icon
        ? React.cloneElement(icon, {
            size:
              size === "xs"
                ? 12
                : size === "sm"
                  ? 12
                  : size === "md"
                    ? 14
                    : size === "lg"
                      ? 16
                      : size === "xl"
                        ? 18
                        : 12,
            className: "oui-text-inherit",
            opacity: loading ? 0 : 1,
          })
        : null;
    }, [size, icon]);

    const content = useMemo(() => {
      if (!leading && !trailing && !iconElement) return children;

      return (
        <Flex as="span" itemAlign={"center"} className="oui-gap-x-1">
          {leading}
          {iconElement}
          <span>{children}</span>
          {trailing}
        </Flex>
      );
    }, [children, leading, trailing, iconElement]);

    const spinnerSize = useMemo(() => {
      switch (size) {
        case "xl":
          return "md";
        case "lg":
          return "md";
        case "md":
          return "sm";
        case "sm":
        case "xs":
          return "xs";
        default:
          return "md";
      }
    }, [size]);

    return (
      <Comp {...rest} disabled={isDisabled} ref={forwardedRef}>
        {loading ? (
          <>
            <span className="oui-invisible">{content}</span>
            <Flex
              itemAlign={"center"}
              justify={"center"}
              position={"absolute"}
              as="span"
            >
              <Spinner size={spinnerSize} color="primaryContrast" />
            </Flex>
          </>
        ) : (
          content
        )}
      </Comp>
    );
  },
);

BaseButton.displayName = "BaseButton";
