import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const CaretDownIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        ref={ref}
        {...props}
        d="M6.007 7.996c-.824 0-1.276.935-.781 1.594l6 8a.994.994 0 0 0 1.593 0l6-8c.495-.66.012-1.594-.812-1.594h-12Z"
      />
    );
  }
);

CaretDownIcon.displayName = "CaretDownIcon";
