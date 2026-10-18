import plugin from "tailwindcss/plugin";

export const gradientPlugin = () =>
  plugin(function ({ addUtilities }) {
    addUtilities({
      ".gradient-primary": {
        backgroundImage:
          "linear-gradient(var(--oui-gradient-primary-angle, 28.29deg), rgb(var(--oui-gradient-primary-start)) var(--oui-gradient-primary-stop-start,0%), rgb(var(--oui-gradient-primary-end)) var(--oui-gradient-primary-stop-end,100%))",
      },
      ".gradient-secondary": {
        backgroundImage:
          "linear-gradient(var(--oui-gradient-secondary-angle, 0deg), rgb(var(--oui-gradient-secondary-start)) var(--oui-gradient-secondary-stop-start,0%), rgb(var(--oui-gradient-secondary-end)) var(--oui-gradient-secondary-stop-end,100%))",
      },
      ".gradient-success": {
        backgroundImage:
          "linear-gradient(var(--oui-gradient-success-angle, 28.29deg), rgb(var(--oui-gradient-success-start)) var(--oui-gradient-success-stop-start,0%), rgba(var(--oui-gradient-success-end)) var(--oui-gradient-success-stop-end,100%))",
      },
      ".gradient-warning": {
        backgroundImage:
          "linear-gradient(var(--oui-gradient-warning-angle, 28.29deg),rgb(var(--oui-gradient-warning-start)) var(--oui-gradient-warning-stop-start,0%), rgb(var(--oui-gradient-warning-end)) var(--oui-gradient-warning-stop-end,100%))",
      },
      ".gradient-danger": {
        backgroundImage:
          "linear-gradient(var(--oui-gradient-danger-angle, 28.29deg), rgb(var(--oui-gradient-danger-start)) var(--oui-gradient-danger-stop-start,0%), rgb(var(--oui-gradient-danger-end)) var(--oui-gradient-danger-stop-end,100%))",
      },
      ".gradient-neutral": {
        backgroundImage:
          "linear-gradient(var(--oui-gradient-neutral-angle, 28.29deg), rgb(var(--oui-gradient-neutral-start)) var(--oui-gradient-neutral-stop-start,0%), rgb(var(--oui-gradient-neutral-end)) var(--oui-gradient-neutral-stop-end,100%))",
      },
      ".gradient-brand": {
        // color: "white",
        backgroundImage:
          "linear-gradient(var(--oui-gradient-brand-angle, 17.44deg), rgb(var(--oui-gradient-brand-start)) var(--oui-gradient-brand-stop-start,0%), rgb(var(--oui-gradient-brand-end)) var(--oui-gradient-brand-stop-end,100%))",
          // "linear-gradient(var(--oui-gradient-angle, 270deg), rgb(var(--oui-gradient-brand-start)) var(--oui-gradient-primary-stop-start,0%), rgb(var(--oui-gradient-brand-end)) var(--oui-gradient-primary-stop-end,100%))",

      },
    });
  });
