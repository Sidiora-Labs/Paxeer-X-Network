import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ArrowDownUpIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M16.007 2.014c-.256 0-.523.086-.718.281L11.6 6.015l1.406 1.405 2-1.968v14.562a1 1 0 002 0V5.452l2 1.968 1.407-1.406-3.688-3.719a1.012 1.012 0 00-.719-.28zm-8 1a1 1 0 00-1 1v14.562l-2-1.968-1.406 1.406 3.688 3.72c.39.39 1.047.39 1.437 0l3.688-3.72-1.407-1.406-2 1.968V4.014a1 1 0 00-1-1z"
        ref={ref}
        {...props}
      />
    );
  }
);

ArrowDownUpIcon.displayName = "ArrowDownUpIconIcon";
