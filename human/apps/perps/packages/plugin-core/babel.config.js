/** Babel config for Jest (TS + React JSX) */
module.exports = {
  env: {
    test: {
      presets: [
        ["@babel/preset-react", { runtime: "automatic" }],
        ["@babel/preset-env", { targets: { node: "current" } }],
        "@babel/preset-typescript",
      ],
    },
  },
};
