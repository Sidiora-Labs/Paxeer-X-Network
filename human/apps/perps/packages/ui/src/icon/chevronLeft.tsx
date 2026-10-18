import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ChevronLeftIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        ref={ref}
        {...props}
        d="M13.778 5.022c-.25.05-.5.176-.653.406l-3.986 6a1.03 1.03 0 0 0 0 1.125l3.986 6c.305.459.943.588 1.401.28.457-.305.585-.946.28-1.405l-3.612-5.438 3.612-5.437c.305-.46.177-1.1-.28-1.406-.229-.154-.498-.176-.748-.125Z"
      />
    );
  }
);

ChevronLeftIcon.displayName = "ChevronLeftIcon";
