import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CheckedSquareFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        fillRule="evenodd"
        clipRule={"evenodd"}
        d="M6.99 2.93a4 4 0 0 0-4 4v10a4 4 0 0 0 4 4h10a4 4 0 0 0 4-4v-10a4 4 0 0 0-4-4h-10Zm9.995 4.382c.227-.217.539-.312.837-.312.298 0 .61.095.836.312a1.102 1.102 0 0 1 0 1.595l-8.15 7.768a1.24 1.24 0 0 1-1.673 0l-3.494-3.33a1.104 1.104 0 0 1 0-1.594 1.24 1.24 0 0 1 1.674 0l2.656 2.531 7.314-6.97Z"
      />
    </BaseIcon>
  );
});

CheckedSquareFillIcon.displayName = "CheckSquareFillIcon";
