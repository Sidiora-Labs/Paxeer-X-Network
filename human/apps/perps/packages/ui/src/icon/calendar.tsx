import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const CalendarIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M8.006 2.014a1 1 0 0 0-1 1 4 4 0 0 0-4 4v10a4 4 0 0 0 4 4h10a4 4 0 0 0 4-4v-10c0-2.205-1.792-4-4-4a1 1 0 0 0-2 0h-6a1 1 0 0 0-1-1Zm-1 3a1 1 0 0 0 2 0h6a1 1 0 0 0 2 0 2 2 0 0 1 2 2v1h-14v-1a2 2 0 0 1 2-2Zm-2 5h14v7a2 2 0 0 1-2 2h-10a2 2 0 0 1-2-2v-7Zm3 2a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm4 0a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm4 0a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm-8 3a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm4 0a1 1 0 1 0 0 2 1 1 0 0 0 0-2Zm4 0a1 1 0 1 0 0 2 1 1 0 0 0 0-2Z"
        ref={ref}
        {...props}
      />
    );
  }
);

CalendarIcon.displayName = "CaretLeftIcon";
