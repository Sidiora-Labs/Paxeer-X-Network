// Object form (Tailwind v4's documented shape): Next accepts it, and so does Vite's
// postcss-load-config, which rejects the array-of-strings form under vitest.
const config = {
  plugins: { "@tailwindcss/postcss": {} },
};

export default config;
