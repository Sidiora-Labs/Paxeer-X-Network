import { FC } from "react";
import { IconProps } from "../utils/types";

export const PinBtn: FC<{
  pinned: boolean;
  size?: number;
  onClick?: (pinned: boolean) => void;
}> = (props) => {
  return (
    <button
      onClick={(e) => {
        props?.onClick?.(!props.pinned);
      }}
    >
      {props.pinned === false ? (
        <UnPinIcon
          size={props.size}
          // className="oui-text-primary-darken hover:oui-text-primary-darken/80"
        />
      ) : (
        <PinnedIcon
          size={props.size}
          className="oui-text-base-contrast-36 hover:oui-text-base-contrast"
        />
      )}
    </button>
  );
};

const PinnedIcon: FC<IconProps> = (props) => {
  const { size = 12 } = props;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path d="M10.008 1.302a.74.74 0 0 0-.486.215c-1.033.988-1.349 1.815-.972 2.947-.88.675-1.437.841-2.536.841-1.503 0-2.484.181-3.152.848v.021a1.583 1.583 0 0 0 0 2.249l1.867 1.881-3.181 3.18c-.26.26-.28.696-.02.956.261.26.699.26.959 0l3.193-3.193 1.87 1.86a1.585 1.585 0 0 0 2.25 0h.02c.668-.667.854-1.522.854-3.144 0-1.03.212-1.758.853-2.523 1.232.361 1.95.015 2.96-.995a.68.68 0 0 0 .188-.479c0-.234-.06-.594-.209-1.041a5.34 5.34 0 0 0-1.312-2.103A5.35 5.35 0 0 0 11.05 1.51c-.448-.149-.808-.208-1.042-.208" />
    </svg>
  );
};

const UnPinIcon: FC<IconProps> = (props) => {
  const { size = 12 } = props;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <path d="M10.008 1.302a.74.74 0 0 0-.486.215c-1.033.988-1.349 1.815-.972 2.947-.88.675-1.437.841-2.536.841-1.503 0-2.484.181-3.152.848v.021a1.583 1.583 0 0 0 0 2.249l1.867 1.881-3.181 3.18c-.26.26-.28.696-.02.956.261.26.699.26.959 0l3.193-3.193 1.87 1.86a1.585 1.585 0 0 0 2.25 0h.02c.668-.667.854-1.522.854-3.144 0-1.03.212-1.758.853-2.523 1.232.361 1.95.015 2.96-.995a.68.68 0 0 0 .188-.479c0-.234-.06-.594-.209-1.041a5.34 5.34 0 0 0-1.312-2.103A5.35 5.35 0 0 0 11.05 1.51c-.448-.149-.808-.208-1.042-.208m.258 1.37c.708.131 1.421.6 1.93 1.108.507.507.94 1.13 1.119 1.944-.636.61-1.026.659-1.662.324a.67.67 0 0 0-.779.116c-1.214 1.213-1.533 2.314-1.533 3.8 0 1.293-.076 1.774-.48 2.207-.113.123-.27.104-.374 0L3.799 7.486a.24.24 0 0 1-.017-.34c.239-.29.769-.514 2.226-.514 1.742.001 2.668-.447 3.812-1.52a.67.67 0 0 0 .125-.77c-.343-.685-.29-1.046.321-1.67" />
    </svg>
  );
};
