import { VariantProps } from "tailwind-variants";
import { Box, BoxProps } from "../box";

import { tv } from "../utils/tv";
import { useEffect, useRef } from "react";

const gridSpanVariants = tv({
  variants: {
    colSpan: {
      auto: "oui-col-auto",
      1: "oui-col-span-1",
      2: "oui-col-span-2",
      3: "oui-col-span-3",
      4: "oui-col-span-4",
      5: "oui-col-span-5",
      6: "oui-col-span-6",
      7: "oui-col-span-7",
      8: "oui-col-span-8",
      9: "oui-col-span-9",
    },
    rowSpan: {
      auto: "oui-row-auto",
      1: "oui-row-span-1",
      2: "oui-row-span-2",
      3: "oui-row-span-3",
      4: "oui-row-span-4",
      5: "oui-row-span-5",
      6: "oui-row-span-6",
      7: "oui-row-span-7",
      8: "oui-row-span-8",
      9: "oui-row-span-9",
    },
  },
});

interface SpanProps extends BoxProps, VariantProps<typeof gridSpanVariants> {}

const Span = (props: SpanProps) => {
  const { colSpan, rowSpan, className, ...rest } = props;
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (ref.current) {
      const parent: HTMLElement = ref.current!.parentElement!;

      if (!parent || !parent.classList.contains("oui-grid")) {
        console.warn("Span component must be a child of a Grid component");
      }
    }
  }, []);

  return (
    <Box
      ref={ref}
      {...rest}
      className={gridSpanVariants({
        colSpan,
        rowSpan,
        className,
      })}
    />
  );
};

export { Span, type SpanProps, gridSpanVariants };
