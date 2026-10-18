import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const PlusIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M11.99 2.93a1 1 0 0 0-1 1v7h-7a1 1 0 0 0 0 2h7v7a1 1 0 0 0 2 0v-7h7a1 1 0 0 0 0-2h-7v-7a1 1 0 0 0-1-1Z"
        />
      </BaseIcon>
    );
  },
);

PlusIcon.displayName = "PlusIcon";
