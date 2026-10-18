import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ArrowLeftRightIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  return (
    <BaseIconWithPath
      d="M21.995 16.007c0-.256-.086-.523-.28-.719l-3.72-3.687-1.406 1.406 1.968 2H3.995a1 1 0 000 2h14.562l-1.968 2 1.406 1.406 3.72-3.687c.194-.196.28-.463.28-.719zm-1-8a1 1 0 00-1-1H5.432l1.97-2-1.407-1.406-3.719 3.687a1.03 1.03 0 000 1.438l3.72 3.687L7.4 11.007l-1.969-2h14.563a1 1 0 001-1z"
      ref={ref}
      {...props}
    />
  );
});

ArrowLeftRightIcon.displayName = "ArrowLeftRightIcon";
