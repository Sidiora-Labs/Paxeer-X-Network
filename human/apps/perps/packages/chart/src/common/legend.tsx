import React, { FC, PropsWithChildren, useMemo } from "react";

export type LegendProps = {};

export const Legend: FC<PropsWithChildren<LegendProps>> = (props) => {
  const children = useMemo(() => {
    if (typeof props.children !== "undefined") {
      return props.children;
    }

    return <text>Legend</text>;
  }, [props.children]);
  return <g transform="translate(5,15)">{children}</g>;
};
