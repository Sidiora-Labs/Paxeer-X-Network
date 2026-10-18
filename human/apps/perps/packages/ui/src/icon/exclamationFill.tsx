import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ExclamationFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        d="M12.014 1.999c-5.523 0-10 4.477-10 10s4.477 10 10 10 10-4.477 10-10-4.477-10-10-10Zm0 5a1 1 0 0 1 1 1v5a1 1 0 0 1-2 0v-5a1 1 0 0 1 1-1Zm0 8a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z"
      />
    </BaseIcon>
  );
});

ExclamationFillIcon.displayName = "ExclamationFillIcon";
