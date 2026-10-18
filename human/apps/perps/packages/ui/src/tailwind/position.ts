import plugin from "tailwindcss/plugin";

export const positionPlugin = () =>
  plugin(function ({ addComponents }) {
    addComponents({
      ".position": {
        left: "var(--oui-left, unset)",
        top: "var(--oui-top, unset)",
        right: "var(--oui-right, unset)",
        bottom: "var(--oui-bottom, unset)",
      },
    });
  });
