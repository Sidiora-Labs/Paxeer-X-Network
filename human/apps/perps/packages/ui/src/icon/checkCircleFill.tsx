import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const CheckedCircleFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  // const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIconWithPath
      ref={ref}
      {...props}
      d="M2.014 11.999c0-5.523 4.477-10 10-10s10 4.477 10 10-4.477 10-10 10-10-4.477-10-10Zm14.971-4.687c.227-.217.539-.312.837-.312.298 0 .61.095.836.312a1.102 1.102 0 0 1 0 1.595l-8.15 7.768a1.24 1.24 0 0 1-1.673 0l-3.494-3.33a1.104 1.104 0 0 1 0-1.594 1.24 1.24 0 0 1 1.674 0l2.656 2.531 7.314-6.97Z"
    ></BaseIconWithPath>
  );
});

CheckedCircleFillIcon.displayName = "CheckedCircleFillIcon";
