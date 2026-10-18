const path = require("node:path");
const fs = require("node:fs");
module.exports = {
 reactStrictMode: true, distDir: process.env.PAXEER_NETWORK_BUILD_DIR || ".next-dev",
 output: "standalone", outputFileTracingRoot: path.join(__dirname, "../.."), turbopack: {},
 transpilePackages: fs.readdirSync(path.join(__dirname,"../../packages")).filter(name => fs.existsSync(path.join(__dirname,"../../packages",name,"package.json"))).map(name => require(path.join(__dirname,"../../packages",name,"package.json")).name),
 images: { unoptimized: true }, experimental: { cpus: 2 },
 webpack(config) { config.externals.push("pino-pretty", "lokijs", "encoding"); return config; }
};
