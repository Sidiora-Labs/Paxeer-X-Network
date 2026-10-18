import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const SquareOutlinedIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        d="M6.99 2.93a4 4 0 0 0-4 4v10a4 4 0 0 0 4 4h10a4 4 0 0 0 4-4v-10a4 4 0 0 0-4-4h-10Zm0 2h10a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2h-10a2 2 0 0 1-2-2v-10a2 2 0 0 1 2-2Z"
      />
    </BaseIcon>
  );
});

SquareOutlinedIcon.displayName = "SquareOutlinedIcon";
