import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const PortfolioActiveIcon = (props: BaseIconProps) => {
  return (
    <BaseIcon {...props}>
      <g id="Portfolio">
        <path
          id="Vector"
          d="M8.9688 13.3124C6.3545 14.0269 4.5 16.3001 4.5 18.9999V20.9999C4.5 21.5522 4.9477 21.9999 5.5 21.9999H19.5C20.0523 21.9999 20.5 21.5522 20.5 20.9999V18.9999C20.5 16.3001 18.6455 14.0269 16.0312 13.3124C15.8071 13.2511 15.5869 13.2794 15.375 13.3749C14.4595 13.7875 13.4849 13.9999 12.5 13.9999C11.5151 13.9999 10.5405 13.7875 9.625 13.3749C9.4131 13.2794 9.1929 13.2511 8.9688 13.3124Z"
          className="oui-fill-base-contrast-36"
        />
        <path
          id="Vector_2"
          d="M12.5 2C9.7386 2 7.5 4.2386 7.5 7C7.5 9.7614 9.7386 12 12.5 12C15.2614 12 17.5 9.7614 17.5 7C17.5 4.2386 15.2614 2 12.5 2Z"
          fill="url(#paint0_linear_148_26239)"
        />
      </g>
      <defs>
        <linearGradient
          id="paint0_linear_148_26239"
          x1="17.5"
          y1="7"
          x2="7.5"
          y2="7"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
          <stop offset="1" stopColor="rgb(var(--oui-gradient-brand-start))" />
        </linearGradient>
      </defs>
    </BaseIcon>
  );
};
