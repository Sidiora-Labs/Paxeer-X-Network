import React, { FC } from "react";
import { SVGProps } from "react";

export interface IconProps extends SVGProps<SVGSVGElement> {
  size: number;
}

export const InfoIcon: FC<IconProps> = (props) => {
  const { size = 20, viewBox, ...rest } = props;
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={`${size}px`}
      height={`${size}px`}
      viewBox={`0 0 20 20`}
      fill="currentColor"
      className="oui-fill-warning-darken"
      {...rest}
    >
      <path
        d="M10.012 1.66602C5.40954 1.66602 1.67871 5.39685 1.67871 9.99935C1.67871 14.6018 5.40954 18.3327 10.012 18.3327C14.6145 18.3327 18.3454 14.6018 18.3454 9.99935C18.3454 5.39685 14.6145 1.66602 10.012 1.66602ZM10.012 5.83268C10.472 5.83268 10.8454 6.20602 10.8454 6.66602V10.8327C10.8454 11.2927 10.472 11.666 10.012 11.666C9.55204 11.666 9.17871 11.2927 9.17871 10.8327V6.66602C9.17871 6.20602 9.55204 5.83268 10.012 5.83268ZM10.012 12.4993C10.472 12.4993 10.8454 12.8727 10.8454 13.3327C10.8454 13.7927 10.472 14.166 10.012 14.166C9.55204 14.166 9.17871 13.7927 9.17871 13.3327C9.17871 12.8727 9.55204 12.4993 10.012 12.4993Z"
        fill="currentColor"
      />
    </svg>
  );
};
