/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // src/api/sdk.ts imports the agent SDK's TypeScript sources, and those modules
  // name each other with the .js specifier their own NodeNext compiler requires.
  // The bundler has to try the TypeScript file behind such a specifier, which is
  // why the build runs on webpack: Turbopack exposes no extension alias.
  experimental: {
    extensionAlias: {
      ".js": [".js", ".ts", ".tsx"],
    },
  },
  async rewrites() {
    const service = process.env.LAYERX_HUMAN_SERVICE_URL;
    if (service === undefined) {
      return [];
    }
    const endpoint = new URL(service);
    if (
      endpoint.protocol !== "https:" ||
      endpoint.username !== "" ||
      endpoint.password !== "" ||
      !["/", "/human", "/human/"].includes(endpoint.pathname) ||
      endpoint.search !== "" ||
      endpoint.hash !== ""
    ) {
      throw new Error("LAYERX_HUMAN_SERVICE_URL must name the HTTPS human service");
    }
    const baseUrl = endpoint.origin;
    return [
      {
        source: "/human/v1/:path*",
        destination: `${baseUrl}/v1/:path*`,
      },
      {
        source: "/v1/:path*",
        destination: `${baseUrl}/v1/:path*`,
      },
      {
        source: "/readyz",
        destination: `${baseUrl}/readyz`,
      },
    ];
  },
  async headers() {
    return [
      {
        source: "/explorer",
        headers: [
          {
            key: "Cache-Control",
            value: "public, s-maxage=60, stale-while-revalidate=300",
          },
        ],
      },
      {
        source: "/explorer/:path*",
        headers: [
          {
            key: "Cache-Control",
            value: "public, s-maxage=60, stale-while-revalidate=300",
          },
        ],
      },
      {
        source: "/api/performance/vitals",
        headers: [{ key: "Cache-Control", value: "no-store" }],
      },
    ];
  },
};

export default nextConfig;
