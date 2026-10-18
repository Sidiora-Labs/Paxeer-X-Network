import React from "react";
import { cn } from "..";
import { BaseIcon, BaseIconProps } from "./baseIcon";

export const ToastErrorIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 1, viewBox = "0 0 20 20", className, ...rest } = props;
    return (
      <BaseIcon
        ref={ref}
        viewBox={viewBox}
        fill="currentColor"
        className={cn(className, "oui-fill-danger")}
        {...rest}
      >
        <path
          d="M10 1.628a8.333 8.333 0 1 0-.001 16.666 8.333 8.333 0 0 0 0-16.666m-2.5 5c.212 0 .435.071.598.234L10 8.762l1.9-1.9a.85.85 0 0 1 .6-.234c.213 0 .436.071.6.234a.86.86 0 0 1 0 1.198l-1.902 1.901 1.901 1.9a.86.86 0 0 1 0 1.2.86.86 0 0 1-1.198 0L10 11.158 8.097 13.06a.86.86 0 0 1-1.198 0 .857.857 0 0 1 0-1.198L8.8 9.96l-1.9-1.9a.857.857 0 0 1 0-1.2.84.84 0 0 1 .6-.233"
          fillOpacity={opacity}
        />
      </BaseIcon>
    );
  },
);
export const ToastSuccessIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 1, viewBox = "0 0 20 20", className, ...rest } = props;
    return (
      <BaseIcon
        ref={ref}
        viewBox={viewBox}
        fill="currentColor"
        className={cn(className, "oui-fill-success")}
        {...rest}
      >
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M1.678 10a8.333 8.333 0 1 1 16.667-.001 8.333 8.333 0 0 1-16.667 0m12.477-3.907c.189-.18.448-.26.697-.26s.507.08.697.26c.38.362.38.969 0 1.33l-6.793 6.473a1.033 1.033 0 0 1-1.394 0l-2.911-2.774a.92.92 0 0 1 0-1.33 1.034 1.034 0 0 1 1.395 0l2.213 2.11z"
        />
      </BaseIcon>
    );
  },
);
export const ToastLoadingIcon = React.forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    const { opacity = 1, viewBox = "0 0 20 20", className, ...rest } = props;
    return (
      <BaseIcon
        ref={ref}
        viewBox={viewBox}
        fill="currentColor"
        className={cn(className, "oui-fill-primary")}
        {...rest}
      >
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M9.167 3.333c0-.46.373-.833.833-.833A7.5 7.5 0 1 1 2.5 10a.833.833 0 0 1 1.667 0A5.834 5.834 0 1 0 10 4.167a.833.833 0 0 1-.833-.834"
          fillOpacity={opacity}
        />
      </BaseIcon>
    );
  },
);

ToastErrorIcon.displayName = "ToasterErrorIcon";
ToastSuccessIcon.displayName = "ToastSuccessIcon";
ToastLoadingIcon.displayName = "ToastLoadingIcon";
