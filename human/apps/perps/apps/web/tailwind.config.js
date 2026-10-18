const load = require("jiti")(__filename);
const base = load("../../packages/ui/tailwind.config.js");
module.exports = { ...base, content: ["./src/**/*.{js,ts,jsx,tsx}", "../../packages/*/src/**/*.{js,ts,jsx,tsx}"] };
