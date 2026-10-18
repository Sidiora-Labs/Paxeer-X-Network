import { forwardRef } from "react";
import type { BaseIconProps } from "./baseIcon";

export const LeftNavVaultsIcon = forwardRef<SVGSVGElement, BaseIconProps>(
  (props, ref) => {
    return (
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="24"
        height="24"
        viewBox="0 0 24 24"
        fill="currentColor"
        focusable={false}
        ref={ref}
        {...props}
      >
        <path
          d="M20 5C20 4.44772 19.5523 4 19 4H5C4.44772 4 4 4.44772 4 5V14C4 14.5523 4.44772 15 5 15H19C19.5523 15 20 14.5523 20 14V5ZM22 14C22 15.6569 20.6569 17 19 17H5C3.34315 17 2 15.6569 2 14V5C2 3.34315 3.34315 2 5 2H19C20.6569 2 22 3.34315 22 5V14Z"
          className="oui-fill-base-contrast-80"
        />
        <path
          d="M7 20C7 19.4477 7.44772 19 8 19H16C16.5523 19 17 19.4477 17 20C17 20.5523 16.5523 21 16 21H8C7.44772 21 7 20.5523 7 20Z"
          className="oui-fill-base-contrast-80"
        />
        <path
          d="M15.2783 6.30759C15.6607 5.90925 16.2939 5.89604 16.6924 6.27829C17.0907 6.66069 17.1039 7.29393 16.7217 7.69235L12.7217 11.8593C12.5331 12.0558 12.2723 12.167 12 12.167C11.7277 12.167 11.4669 12.0558 11.2783 11.8593L10.3994 10.9433L8.72165 12.6924C8.33925 13.0907 7.70601 13.1039 7.30759 12.7217C6.90925 12.3392 6.89604 11.706 7.27829 11.3076L9.67868 8.80759L9.7529 8.73825C9.93282 8.5854 10.1621 8.49997 10.4004 8.49997C10.6725 8.50008 10.9326 8.61129 11.1211 8.80759L11.999 9.72263L15.2783 6.30759Z"
          className="oui-fill-base-contrast-80"
        />
      </svg>
    );
  },
);
