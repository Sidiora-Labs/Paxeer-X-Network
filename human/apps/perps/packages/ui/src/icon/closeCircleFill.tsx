import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const CloseCircleFillIcon = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  const { opacity = 0.54, ...rest } = props;
  return (
    <BaseIcon ref={ref} {...rest}>
      <path
        fill="currentcolor"
        fillOpacity={opacity}
        d="M11.999 1.953c-5.523 0-10 4.477-10 10s4.477 10 10 10 10-4.477 10-10-4.477-10-10-10Zm-3 6c.256 0 .523.086.719.281l2.28 2.281 2.282-2.28c.196-.196.463-.282.719-.282.256 0 .523.086.719.281a1.03 1.03 0 0 1 0 1.438l-2.281 2.281 2.28 2.281a1.03 1.03 0 0 1 0 1.438 1.03 1.03 0 0 1-1.437 0l-2.281-2.28-2.281 2.28a1.03 1.03 0 0 1-1.438 0 1.029 1.029 0 0 1 0-1.438l2.28-2.28-2.28-2.282a1.029 1.029 0 0 1 0-1.438c.196-.195.463-.28.719-.28Z"
      />
    </BaseIcon>
  );
});

CloseCircleFillIcon.displayName = "CloseCircleFillIcon";
