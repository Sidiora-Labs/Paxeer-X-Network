import React from "react";
import type { BaseIconProps } from "./baseIcon";

export const InfoFillIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { className, opacity = 1, ...rest } = props;

    return (
      <svg
        width="20"
        height="20"
        viewBox="0 0 20 20"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        opacity={opacity}
        className={className}
        ref={ref}
        {...rest}
      >
        <path
          d="M10 2.5C5.86 2.5 2.5 5.86 2.5 10C2.5 14.14 5.86 17.5 10 17.5C14.14 17.5 17.5 14.14 17.5 10C17.5 5.86 14.14 2.5 10 2.5ZM10.75 13.75L9.25 13.75L9.25 9.25H10.75L10.75 13.75ZM10.75 7.75H9.25V6.25H10.75V7.75Z"
          fill="currentColor"
        />
      </svg>
    );
  },
);

InfoFillIcon.displayName = "InfoFillIcon";
