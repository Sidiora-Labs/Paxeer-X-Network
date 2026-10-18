import { tv, type VariantProps } from "tailwind-variants";
import { Box } from "../box";
import { Flex } from "../flex";
import { Text } from "../typography";

const dotStatusVariants = tv({
  slots: {
    root: "oui-flex oui-items-center oui-justify-center oui-gap-1",
    dot: "oui-flex-shrink-0 oui-rounded-full",
    label: "oui-text-2xs",
  },
  variants: {
    color: {
      primary: {
        dot: "oui-bg-primary-darken",
        label: "oui-text-primary-darken",
      },
      warning: {
        dot: "oui-bg-warning-darken",
        label: "oui-text-warning-darken",
      },
      profit: {
        dot: "oui-bg-profit-darken",
        label: "oui-text-profit-darken",
      },
      loss: {
        dot: "oui-bg-loss-darken",
        label: "oui-text-loss-darken",
      },
      secondary: {
        dot: "oui-bg-secondary-darken",
        label: "oui-text-secondary-darken",
      },
    },
    size: {
      xs: {
        dot: "oui-w-1 oui-h-1",
        label: "oui-text-2xs",
      },
      sm: {
        dot: "oui-w-2 oui-h-2",
        label: "oui-text-2xs",
      },
      md: {
        dot: "oui-w-3 oui-h-3",
        label: "oui-text-2xs",
      },
      lg: {
        dot: "oui-w-4 oui-h-4",
        label: "oui-text-2xs",
      },
    },
  },
  defaultVariants: {
    color: "primary",
    size: "xs",
  },
});

const DotStatus = (
  props: VariantProps<typeof dotStatusVariants> & {
    label?: string;
    classNames?: {
      root?: string;
      dot?: string;
      label?: string;
    };
  },
) => {
  const { color, size, label, classNames } = props;
  const { root, dot, label: labelSlot } = dotStatusVariants({ color, size });
  if (!label) {
    return <Box r="full" className={dot({ className: classNames?.dot })} />;
  }

  return (
    <Flex
      itemAlign={"center"}
      justify={"center"}
      gapX={1}
      className={root({ className: classNames?.root })}
    >
      <Box r="full" className={dot({ className: classNames?.dot })} />

      <Text size="2xs" className={labelSlot({ className: classNames?.label })}>
        {label}
      </Text>
    </Flex>
  );
};

DotStatus.displayName = "DotStatus";
export { DotStatus, dotStatusVariants };
