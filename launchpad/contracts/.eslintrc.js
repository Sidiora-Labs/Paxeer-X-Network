module.exports = {
  root: true,
  env: { node: true, es2022: true, mocha: true },
  parserOptions: { ecmaVersion: "latest", sourceType: "script" },
  extends: ["eslint:recommended"],
  rules: { "no-unused-vars": "warn", "no-empty": ["error", { "allowEmptyCatch": true }] },
  ignorePatterns: ["node_modules/", "artifacts/", "cache/", "coverage/", "typechain-types/", "docs/", "/lib/"]
};
