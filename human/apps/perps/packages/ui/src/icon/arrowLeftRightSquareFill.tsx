import React from "react";
import type { BaseIconProps } from "./baseIcon";

export const ArrowLeftRightSquareFill = React.forwardRef<
  SVGSVGElement,
  BaseIconProps
>((props, ref) => {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={14}
      height={14}
      viewBox="0 0 14 14"
      fill="currentColor"
      focusable={false}
      ref={ref}
      {...props}
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M1.74463 4.07796C1.74463 2.78938 2.78938 1.74463 4.07796 1.74463H9.9113C11.1999 1.74463 12.2446 2.78938 12.2446 4.07796V9.9113C12.2446 11.1999 11.1999 12.2446 9.9113 12.2446H4.07796C2.78938 12.2446 1.74463 11.1999 1.74463 9.9113V4.07796ZM6.5 5.5C6.224 5.5 6 5.724 6 6C6 6.276 6.224 6.5 6.5 6.5H9V8L11 6L9 4V5.5H6.5ZM7.5 7.5C7.776 7.5 8 7.724 8 8C8 8.276 7.776 8.5 7.5 8.5H5V10L3 8L5 6V7.5H7.5Z"
      />
    </svg>
  );
});

if (process.env.NODE_ENV !== "production") {
  ArrowLeftRightSquareFill.displayName = "ArrowLeftRightSquareFill";
}
