import { FC } from "react";
import { tv, type VariantProps } from "tailwind-variants";
import {
  ComponentPropsWithout,
  RemovedProps,
} from "../helpers/component-props";

const spinnerVariants = tv({
  base: "oui-text-gray-200 oui-animate-spin dark:oui-text-gray-600 oui-fill-primary-darken",
  variants: {
    size: {
      xs: "oui-w-3 oui-h-3",
      sm: "oui-w-4 oui-h-4",
      md: "oui-w-6 oui-h-6",
      lg: "oui-w-8 oui-h-8",
      xl: "oui-w-10 oui-h-10",
    },
    color: {
      primary: "oui-fill-primary-darken",
      primaryContrast: "oui-fill-primary-contrast",
      success: "oui-fill-success",
      danger: "oui-fill-danger",
      warning: "oui-fill-warning-darken",
      gray: "oui-fill-gray",
      darkGray: "oui-fill-darkGray",
      white: "oui-fill-base-contrast",
    },
    // background: {
    //   default: "oui-text-base-contrast/40",
    //   transparent: "oui-text-transparent",
    // },
  },

  defaultVariants: {
    size: "lg",
    color: "primary",
  },
});

interface SpinnerProps
  extends
    ComponentPropsWithout<"svg", RemovedProps>,
    VariantProps<typeof spinnerVariants> {
  loading?: boolean;
}

const Spinner: FC<SpinnerProps> = (props) => {
  const { size, color, loading = true, children, className } = props;
  if (!loading) {
    return <>{children}</>;
  }
  return (
    <span role="status" className="oui-inline-block">
      <svg
        aria-hidden="true"
        className={spinnerVariants({ size, className, color })}
        viewBox="0 0 100 101"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          d="M100 50.5908C100 78.2051 77.6142 100.591 50 100.591C22.3858 100.591 0 78.2051 0 50.5908C0 22.9766 22.3858 0.59082 50 0.59082C77.6142 0.59082 100 22.9766 100 50.5908ZM9.08144 50.5908C9.08144 73.1895 27.4013 91.5094 50 91.5094C72.5987 91.5094 90.9186 73.1895 90.9186 50.5908C90.9186 27.9921 72.5987 9.67226 50 9.67226C27.4013 9.67226 9.08144 27.9921 9.08144 50.5908Z"
          fill="currentFill"
          fillOpacity={0.2}
        />
        <path
          d="M93.9676 39.0409C96.393 38.4038 97.8624 35.9116 97.0079 33.5539C95.2932 28.8227 92.871 24.3692 89.8167 20.348C85.8452 15.1192 80.8826 10.7238 75.2124 7.41289C69.5422 4.10194 63.2754 1.94025 56.7698 1.05124C51.7666 0.367541 46.6976 0.446843 41.7345 1.27873C39.2613 1.69328 37.813 4.19778 38.4501 6.62326C39.0873 9.04874 41.5694 10.4717 44.0505 10.1071C47.8511 9.54855 51.7191 9.52689 55.5402 10.0491C60.8642 10.7766 65.9928 12.5457 70.6331 15.2552C75.2735 17.9648 79.3347 21.5619 82.5849 25.841C84.9175 28.9121 86.7997 32.2913 88.1811 35.8758C89.083 38.2158 91.5421 39.6781 93.9676 39.0409Z"
          fill="currentFill"
        />
      </svg>
      {/* hide, not show when render */}
      <span className="oui-sr-only">Loading...</span>
    </span>
  );
};

export { Spinner, spinnerVariants };

export type { SpinnerProps };
