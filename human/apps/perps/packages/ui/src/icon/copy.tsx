import React from "react";
import { BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const CopyIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        ref={ref}
        {...props}
        d="M6.998 2.99a4 4 0 00-4 4v6a4 4 0 004 4 4 4 0 004 4h6a4 4 0 004-4v-6a4 4 0 00-4-4 4 4 0 00-4-4h-6zm10 6a2 2 0 012 2v6a2 2 0 01-2 2h-6a2 2 0 01-2-2h4a4 4 0 004-4v-4z"
      />
    );
  }
);

CopyIcon.displayName = "CopyIcon";
