import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ArrowDownSquareFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        d="M6.99 2.99a4 4 0 00-4 4v10a4 4 0 004 4h10a4 4 0 004-4v-10a4 4 0 00-4-4h-10zm5 4a1 1 0 011 1v5h3l-4 4-4-4h3v-5a1 1 0 011-1z"
      />
    </BaseIcon>
  );
});

ArrowDownSquareFillIcon.displayName = "ArrowDownSquareFillIconIcon";
