import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ArrowUpSquareFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  return (
    <BaseIconWithPath
      ref={ref}
      {...props}
      d="M6.99 20.99a4 4 0 01-4-4v-10a4 4 0 014-4h10a4 4 0 014 4v10a4 4 0 01-4 4h-10zm5-4a1 1 0 001-1v-5h3l-4-4-4 4h3v5a1 1 0 001 1z"
    ></BaseIconWithPath>
  );
});

ArrowUpSquareFillIcon.displayName = "ArrowUpSquareFillIconIcon";
