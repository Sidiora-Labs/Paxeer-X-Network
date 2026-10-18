module.exports = {
  env:{
    test:{
      presets:[
          ["@babel/preset-react",{ runtime: "automatic" }],
        ["@babel/preset-env", { targets: { node: "current" } }],
        "@babel/preset-typescript",
      ]
    }
  },
  };
  // presets: [
  //
  //   ["@babel/preset-env", { targets: { node: "current" } }],
  //   "@babel/preset-typescript",
  // ],
// };
