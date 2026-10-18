import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ReduceIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <rect
          x="4"
          y="11"
          width="16"
          height="1.5"
          rx="0.666667"
          fill="currentcolor"
          fillOpacity={opacity}
        />
      </BaseIcon>
    );
  },
);

ReduceIcon.displayName = "ReduceIcon";
