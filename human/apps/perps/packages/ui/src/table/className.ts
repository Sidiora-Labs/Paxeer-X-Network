import { tv } from "tailwind-variants";

export const alignVariants = tv({
  variants: {
    align: {
      left: "oui-text-start",
      start: "oui-text-start",
      center: "oui-text-center",
      right: "oui-text-end",
      end: "oui-text-end",
    },
  },
  defaultVariants: {
    align: "left",
  },
});
