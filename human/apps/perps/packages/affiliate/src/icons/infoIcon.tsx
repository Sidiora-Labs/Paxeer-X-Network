import { FC, SVGProps } from "react";

export interface IconProps extends SVGProps<SVGSVGElement> {
  size?: number;
}

export const InfoIcon: FC<IconProps> = (props) => {
  const { size = 12, className, ...rest } = props;
  return (
    <svg
      width={`${size}`}
      height={`${size}`}
      viewBox="0 0 12 12"
      xmlns="http://www.w3.org/2000/svg"
      fill="currenColor"
      // fillOpacity="0.36"
      className={className}
      {...rest}
    >
      <path d="M0 6a6 6 0 1 1 12 0A6 6 0 0 1 0 6m6.714-2.25V2.321H5.286V3.75zm-.058 1.125H5.344V9.75h1.312z" />
    </svg>
  );
};
