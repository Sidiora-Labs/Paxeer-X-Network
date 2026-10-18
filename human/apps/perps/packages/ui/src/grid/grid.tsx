import React from "react";
import { tv } from "../utils/tv";
import { VariantProps } from "tailwind-variants";
import { type BoxProps, Box } from "../box";
import { gapVariants } from "../layout/gap";

const gridVariants = tv({
  extend: gapVariants,
  base: ["oui-grid"],
  variants: {
    // inline: {
    //   true: "oui-inline-grid",
    //   // false: "",
    // },
    cols: {
      1: "oui-grid-cols-1",
      2: "oui-grid-cols-2",
      3: "oui-grid-cols-3",
      4: "oui-grid-cols-4",
      5: "oui-grid-cols-5",
      6: "oui-grid-cols-6",
      7: "oui-grid-cols-7",
      8: "oui-grid-cols-8",
      9: "oui-grid-cols-9",
      none: "oui-grid-cols-none",
    },
    rows: {
      1: "oui-grid-rows-1",
      2: "oui-grid-rows-2",
      3: "oui-grid-rows-3",
      4: "oui-grid-rows-4",
      5: "oui-grid-rows-5",
      6: "oui-grid-rows-6",
      7: "oui-grid-rows-7",
      8: "oui-grid-rows-8",
      9: "oui-grid-rows-9",
      none: "oui-grid-rows-none",
    },
    autoFlow: {
      row: "oui-grid-flow-row",
      col: "oui-grid-flow-col",
      rowDense: "oui-grid-flow-row-dense",
      colDense: "oui-grid-flow-col-dense",
    },
    gap: {
      0: "oui-gap-0",
      1: "oui-gap-1",
      2: "oui-gap-2",
      3: "oui-gap-3",
      4: "oui-gap-4",
      5: "oui-gap-5",
    },
    gapX: {
      0: "oui-gap-x-0",
      1: "oui-gap-x-1",
      2: "oui-gap-x-2",
      3: "oui-gap-x-3",
      4: "oui-gap-x-4",
      5: "oui-gap-x-5",
    },
    gapY: {
      0: "oui-gap-y-0",
      1: "oui-gap-y-1",
      2: "oui-gap-y-2",
      3: "oui-gap-y-3",
      4: "oui-gap-y-4",
      5: "oui-gap-y-5",
    },
  },
  defaultVariants: {
    // cols: "1",
    // rows: "1",
    // gap: 4,
    // autoFlow: "row",
  },
});

interface GridProps extends BoxProps, VariantProps<typeof gridVariants> {}

const Grid = React.forwardRef<HTMLDivElement, GridProps>((props, ref) => {
  const { className, cols, rows, gap, gapX, gapY, autoFlow, ...rest } = props;

  return (
    <Box
      ref={ref}
      {...rest}
      className={gridVariants({
        className,
        cols,
        rows,
        gap,
        gapX,
        gapY,
        autoFlow,
      })}
    />
  );
});

Grid.displayName = "Grid";

export { Grid, gridVariants };
