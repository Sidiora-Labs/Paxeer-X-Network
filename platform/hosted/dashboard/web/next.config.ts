import path from "node:path";
import type { NextConfig } from "next";

const repositoryRoot = path.resolve(import.meta.dirname, "../../../..");

const config: NextConfig = {
  output: "standalone",
  outputFileTracingRoot: repositoryRoot,
  poweredByHeader: false,
  reactStrictMode: true,
  turbopack: {
    root: repositoryRoot,
  },
  async rewrites() {
    return [
      {
        source: "/v1/dashboard/:path*",
        destination: "http://dashboard.railway.internal:9445/v1/dashboard/:path*",
      },
    ];
  },
};

export default config;
