import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CircleOutlinedIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        d="M12.014 1.999c-5.523 0-10 4.477-10 10s4.477 10 10 10 10-4.477 10-10-4.477-10-10-10Zm0 2a8 8 0 1 1 0 16 8 8 0 0 1 0-16Z"
      />
    </BaseIcon>
  );
});

CircleOutlinedIcon.displayName = "CircleOutlinedIcon";
