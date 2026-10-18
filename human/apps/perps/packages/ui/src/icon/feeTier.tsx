import React from "react";
import { BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const FeeTierIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M7.998 2.014a4 4 0 00-4 4v12a4 4 0 004 4h8a4 4 0 004-4v-12a4 4 0 00-4-4h-8zm0 2h8a2 2 0 012 2v12a2 2 0 01-2 2h-8a2 2 0 01-2-2v-12a2 2 0 012-2zm1 2a1 1 0 00-1 1v1a1 1 0 001 1h6a1 1 0 001-1v-1a1 1 0 00-1-1h-6zm0 4a1 1 0 100 2 1 1 0 000-2zm3 0a1 1 0 100 2 1 1 0 000-2zm3 0a1 1 0 100 2 1 1 0 000-2zm-6 3a1 1 0 100 2 1 1 0 000-2zm3 0a1 1 0 100 2 1 1 0 000-2zm3 0a1 1 0 00-1 1v3a1 1 0 002 0v-3a1 1 0 00-1-1zm-6 3a1 1 0 100 2 1 1 0 000-2zm3 0a1 1 0 100 2 1 1 0 000-2z"
        ref={ref}
        {...props}
      />
    );
  }
);

FeeTierIcon.displayName = "FeeTierIconIcon";
