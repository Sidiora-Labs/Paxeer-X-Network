const path = require("path");

/** @type {import('tailwindcss').Config} */
module.exports = {
    content: ["./src/**/*.{ts,js,tsx,jsx,mdx}",
    ],
    presets: [require(path.resolve(__dirname, "../ui/tailwind.config.js"))],
};
