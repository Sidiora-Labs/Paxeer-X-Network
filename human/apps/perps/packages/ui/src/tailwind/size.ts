import plugin from "tailwindcss/plugin";

export const sizePlugin = () =>
  plugin(function ({ addComponents }) {
    addComponents({
      ".size-height": {
        height: "var(--oui-height, unset)",
      },
      ".size-width": {
        width: "var(--oui-width, unset)",
      },
    });
  });
