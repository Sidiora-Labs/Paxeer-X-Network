import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ArrowRightUpSquareFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  return (
    <BaseIconWithPath
      d="M12.7432 15.743C14.3999 15.743 15.7432 14.3998 15.7432 12.743V5.24304C15.7432 3.58629 14.3999 2.24304 12.7432 2.24304H5.24316C3.58641 2.24304 2.24316 3.58629 2.24316 5.24304V12.743C2.24316 14.3998 3.58641 15.743 5.24316 15.743H12.7432ZM6.74316 11.993C6.55116 11.993 6.35092 11.9285 6.20392 11.7823C5.91142 11.489 5.91142 10.997 6.20392 10.7038L9.20392 7.7038L7.49316 5.99304H11.9932V10.493L10.2824 8.78229L7.28241 11.7823C7.13616 11.9285 6.93516 11.993 6.74316 11.993Z"
      ref={ref}
      {...props}
    />
  );
});

ArrowRightUpSquareFillIcon.displayName = "ArrowRightUpSquareFillIcon";
