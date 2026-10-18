import plugin from "tailwindcss/plugin";

export const scrollBarPlugin = () =>
  plugin(function ({ addComponents }) {
    const customVerticalScrollbar = {
      ".custom-scrollbar::-webkit-scrollbar": {
        width: "6px",
        height: "6px",
      },

      ".custom-scrollbar::-webkit-scrollbar-track": {
        backgroundColor: "transparent",
        borderRadius: "4px",
        paddingTop: "20px",
      },

      ".custom-scrollbar::-webkit-scrollbar-thumb": {
        borderRadius: "3px",
        borderStyle: "dashed",
        borderColor: "transparent",
        borderWidth: "1px",
        backgroundColor: "rgb(var(--oui-color-base-7))",
        backgroundClip: "padding-box",
      },

      ".custom-scrollbar::-webkit-scrollbar-thumb:hover": {
        background: "rgb(var(--oui-color-base-5))",
        borderRadius: "3px",
      },

      ".custom-scrollbar::-webkit-scrollbar-corner": {
        backgroundColor: "transparent",
      },
    };

    addComponents(customVerticalScrollbar);

    const hideScrollBar = {
      ".hide-scrollbar::-webkit-scrollbar": {
        display: "none",
      },
      ".hide-scrollbar::-webkit-scrollbar-thumb": {
        display: "none",
      },
    };

    addComponents(hideScrollBar);
  });
