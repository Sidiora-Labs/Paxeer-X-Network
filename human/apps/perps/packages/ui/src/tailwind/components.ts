import plugin from "tailwindcss/plugin";

export const componentsPlugin = () =>
  plugin(function ({ addComponents, addBase }) {
    const components = {
      ".card": {
        color: "red",
      },
      ".card:has(.header-list)": {
        paddingTop: "0px",
        paddingBottom: "0px",
      },
    };

    addComponents(components);
  });
