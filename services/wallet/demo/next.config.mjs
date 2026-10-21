/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Transpile the workspace SDK so Next can resolve its TS sources during dev.
  transpilePackages: ['@paxeer/wallet'],
  experimental: {
    // Larger payload caps if a partner app passes complex tx data.
    serverActions: { bodySizeLimit: '2mb' },
  },
};

export default nextConfig;
