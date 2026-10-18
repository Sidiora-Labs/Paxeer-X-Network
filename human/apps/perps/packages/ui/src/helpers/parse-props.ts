import { CSSProperties } from "react";

export const parseSizeProps = <
  T extends {
    width?: number | string;
    height?: number | string;
    angle?: number;
    left?: number | string;
    right?: number | string;
    top?: number | string;
    bottom?: number | string;
    [key: string]: any;
  },
>(
  props: T,
) => {
  const { width, height, angle, left, top, bottom, right, ...rest } = props;

  const style = Object.create(null);

  if (angle) {
    style["--oui-gradient-angle"] = `${angle}deg`;
  }

  convertToStyle("--oui-width", width, style);
  convertToStyle("--oui-height", height, style);
  convertToStyle("--oui-left", left, style);
  convertToStyle("--oui-right", right, style);
  convertToStyle("--oui-top", top, style);
  convertToStyle("--oui-bottom", bottom, style);

  return {
    ...rest,
    style: {
      ...rest.style,
      ...style,
    },
  } as const;
};

function convertToStyle(
  key: string,
  value: string | number | undefined,
  style: CSSProperties,
) {
  if (typeof value !== "undefined") {
    (style as Record<string,string|number>)[key] = convertToPx(value);
  }
}

function convertToPx(value: number | string) {
  return typeof value === "number" ? `${value}px` : value;
}

export const parseAngleProps = (props: { angle?: number }) => {
  const { angle } = props;
  return {
    "--oui-gradient-angle": angle ? `${angle}deg` : `180deg`,
  };
};
