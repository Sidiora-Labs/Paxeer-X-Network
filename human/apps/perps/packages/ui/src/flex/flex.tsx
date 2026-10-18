import { ElementRef, forwardRef } from "react";
import { gapVariants } from "../layout/gap";
import { VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";
import { Box, BoxProps } from "../box";

type FlexElement = ElementRef<"div">;

// interface CommonFlexProps extends MarginProps, LayoutProps, FlexOwnProps {}
// type FlexDivProps = { as?: 'div' } & ComponentPropsWithout<'div', RemovedProps>;
// type FlexSpanProps = { as: 'span' } & ComponentPropsWithout<'span', RemovedProps>;
// type FlexProps = CommonFlexProps & (FlexSpanProps | FlexDivProps);

const flexBaseVariant = tv({
  variants: {
    ...gapVariants.variants,
    // ...layoutVariants.variants,
    // ...positionVariants.variants,
  },
});

const flexVariant = tv(
  {
    extend: flexBaseVariant,
    base: ["oui-flex"],
    variants: {
      display: {
        flex: "oui-flex",
        inlineFlex: "oui-inline-flex",
      },
      direction: {
        row: "oui-flex-row",
        rowReverse: "oui-flex-row-reverse",
        column: "oui-flex-col",
        columnReverse: "oui-flex-col-reverse",
      },
      itemAlign: {
        start: "oui-items-start",
        end: "oui-items-end",
        center: "oui-items-center",
        baseline: "oui-items-baseline",
        stretch: "oui-items-stretch",
      },
      justify: {
        start: "oui-justify-start",
        end: "oui-justify-end",
        center: "oui-justify-center",
        between: "oui-justify-between",
        around: "oui-justify-around",
        evenly: "oui-justify-evenly",
        stretch: "oui-justify-stretch",
      },
      wrap: {
        noWrap: "oui-flex-nowrap",
        wrap: "oui-flex-wrap",
        wrapReverse: "oui-flex-wrap-reverse",
      },
    },
    defaultVariants: {
      display: "flex",
      direction: "row",
      itemAlign: "center",
      justify: "start",
      wrap: "noWrap",
      // gap: 4,
      // gap: 0,
      // gapX: 0,
      // gapY: 0,
    },
  },
  {
    responsiveVariants: true,
  }
);

// const flexVariant = compose(layoutVariants, gapVariants, flexBaseVariant);
interface FlexProps extends BoxProps, VariantProps<typeof flexVariant> {
  // asChild?: boolean;
  // as?: "div" | "span";
  // width?: string | number;
  // height?: string | number;
}

const Flex = forwardRef<FlexElement, FlexProps>((props, ref) => {
  const {
    className,
    display,
    gap,
    gapX,
    gapY,
    wrap,
    justify,
    itemAlign,
    direction,
    ...rest
  } = props;
  // const Comp = asChild ? Slot : TAG;
  //

  return (
    <Box
      ref={ref}
      className={flexVariant({
        className,
        display,
        gap,
        gapX,
        gapY,
        wrap,
        justify,
        itemAlign,
        direction,
      })}
      {...rest}
    />
  );
});

Flex.displayName = "Flex";

export { Flex, flexVariant };

export type { FlexProps };
