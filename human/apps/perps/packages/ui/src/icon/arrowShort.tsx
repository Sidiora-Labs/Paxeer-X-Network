import React from "react";
import { BaseIcon, BaseIconProps } from "./baseIcon";
import { BaseIconWithPath } from "./baseIconWithPath";

export const ArrowUpShortIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M11.993 18.012a1 1 0 0 1-.999-.999V9.458l-2.997 2.966-1.405-1.405 4.683-4.714a1 1 0 0 1 .72-.293c.26 0 .521.098.716.293l4.683 4.714-1.405 1.405-2.997-2.966v7.555a1 1 0 0 1-.999.999"
        ref={ref}
        {...props}
      />
    );
  }
);


export const ArrowDownShortIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M11.993 6.012a1 1 0 0 0-1 .999v7.555L7.998 11.6l-1.405 1.405 4.683 4.714c.195.196.457.293.719.293.26 0 .522-.098.717-.293l4.683-4.714L15.99 11.6l-2.997 2.966V7.011a1 1 0 0 0-1-.999"
        ref={ref}
        {...props}
      />
    );
  }
);

export const ArrowLeftShortIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M18.012 11.993a1 1 0 0 0-.999-1H9.458l2.966-2.996-1.405-1.405-4.714 4.683a1 1 0 0 0-.293.719c0 .26.098.522.293.717l4.714 4.683 1.405-1.405-2.966-2.997h7.555a1 1 0 0 0 .999-1"
        ref={ref}
        {...props}
      />
    );
  }
);

export const ArrowRightShortIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <BaseIconWithPath
        d="M6.012 11.993a1 1 0 0 1 .999-1h7.555L11.6 7.998l1.405-1.405 4.714 4.683c.196.195.293.457.293.719 0 .26-.098.522-.293.717l-4.714 4.683L11.6 15.99l2.966-2.997H7.011a1 1 0 0 1-.999-1"
        ref={ref}
        {...props}
      />
    );
  }
);

ArrowUpShortIcon.displayName = "ArrowUpShortIcon";
ArrowDownShortIcon.displayName = "ArrowDownShortIcon";
ArrowLeftShortIcon.displayName = "ArrowLeftShortIcon";
ArrowRightShortIcon.displayName = "ArrowRightShortIcon";
