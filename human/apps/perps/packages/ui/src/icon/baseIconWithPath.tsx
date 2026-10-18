import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export interface BaseIconWithPathProps extends BaseIconProps {
  d: string;
}

export const BaseIconWithPath = React.forwardRef<
  SVGSVGElement,
  BaseIconWithPathProps
>((props, ref) => {
  const { opacity = 0.54, d, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        fillRule="evenodd"
        clipRule={"evenodd"}
        d={d}
      />
    </BaseIcon>
  );
});
