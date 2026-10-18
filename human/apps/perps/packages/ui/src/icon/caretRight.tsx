import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CaretRightIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 0.54, ...rest } = props;
    return (
      <BaseIcon ref={ref} {...rest}>
        <path
          fill="currentcolor"
          fillOpacity={opacity}
          d="M8.558 5.113a1.015 1.015 0 0 0-.562.906v12c0 .824.966 1.307 1.625.812l8-6c.533-.4.533-1.224 0-1.624l-8-6a1.014 1.014 0 0 0-1.063-.094Z"
        />
      </BaseIcon>
    );
  }
);

CaretRightIcon.displayName = "CaretRightIcon";
