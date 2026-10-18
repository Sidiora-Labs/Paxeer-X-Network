export type Gradient = {
  start: string;
  end: string;
  degree: number;
  stops: number;
};

export const decorationVariants = {
  variants: {
    border: {
      true: "oui-border oui-border-line",
      // false: "oui-border-none",
    },
    // borderTop
    r: {
      none: "oui-rounded-none",
      sm: "oui-rounded-sm",
      base: "oui-rounded",
      md: "oui-rounded-md",
      lg: "oui-rounded-lg",
      xl: "oui-rounded-xl",
      "2xl": "oui-rounded-2xl",
      full: "oui-rounded-full",
    },
    gradient: {
      //   // brands:'',
      // primary: "oui-from-primary-400 to-primary-900",
      primary: "oui-gradient-primary",
      secondary: "oui-gradient-secondary",
      brand: "oui-gradient-brand",
      success: "oui-gradient-success",
      warning: "oui-gradient-warning",
      danger: "oui-gradient-danger",
      neutral: "oui-gradient-neutral",
    },
    intensity: {
      100: "oui-bg-base-1",
      200: "oui-bg-base-2",
      300: "oui-bg-base-3",
      400: "oui-bg-base-4",
      500: "oui-bg-base-5",
      600: "oui-bg-base-6",
      700: "oui-bg-base-7",
      800: "oui-bg-base-8",
      900: "oui-bg-base-9",
    },
    borderColor: {
      4: "oui-border-line-4",
      6: "oui-border-line-6",
      8: "oui-border-line",
      12: "oui-border-line-12",
      16: "oui-border-line-16",
    },
  },
};
