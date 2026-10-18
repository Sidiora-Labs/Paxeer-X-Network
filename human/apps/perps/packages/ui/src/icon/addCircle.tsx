import React from "react";
import { forwardRef, SVGProps } from "react";
import type { BaseIconProps } from "./baseIcon";

export const AddCircleIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { className, opacity = 0.54, ...rest } = props;
    return (
      <svg
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="currentColor"
        xmlns="http://www.w3.org/2000/svg"
        opacity={opacity}
        className={className}
        ref={ref}
        {...rest}
      >
        <path
          d="M8.47 4.669c.12.12.205.287.205.471v2.15h2.151c.184.001.35.087.471.207a.68.68 0 0 1 .207.472c0 .368-.31.677-.678.678h-2.15v2.15c0 .368-.31.678-.678.678a.687.687 0 0 1-.678-.678v-2.15H5.17a.686.686 0 0 1-.679-.678c0-.368.31-.678.678-.678h2.15V5.14a.687.687 0 0 1 .679-.678c.184 0 .35.085.471.206"
          fill="currentColor"
        />
        <path
          d="M7.999 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1 0 10.667 5.334 5.334 0 0 1 0-10.667"
          fill="currentColor"
        />
      </svg>
    );
  },
);

AddCircleIcon.displayName = "AddCircleIcon";

export default AddCircleIcon;
