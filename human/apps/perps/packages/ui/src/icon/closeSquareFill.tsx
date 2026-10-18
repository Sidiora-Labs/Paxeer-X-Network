import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CloseSquareFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        d="M7.006 2.93a4 4 0 0 0-4 4v10a4 4 0 0 0 4 4h10a4 4 0 0 0 4-4v-10a4 4 0 0 0-4-4h-10Zm2 5c.256 0 .523.086.719.28l2.28 2.282 2.282-2.281c.195-.195.463-.281.719-.281.256 0 .523.086.719.28a1.03 1.03 0 0 1 0 1.439l-2.281 2.28 2.28 2.282a1.03 1.03 0 0 1 0 1.438 1.03 1.03 0 0 1-1.437 0l-2.281-2.281-2.281 2.28a1.03 1.03 0 0 1-1.438 0 1.03 1.03 0 0 1 0-1.437l2.28-2.281-2.28-2.281a1.03 1.03 0 0 1 0-1.438c.195-.195.463-.281.719-.281Z"
      />
    </BaseIcon>
  );
});

CloseSquareFillIcon.displayName = "CloseSquareFillIcon";
