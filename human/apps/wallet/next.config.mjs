import { withSentryConfig } from '@sentry/nextjs';

/** @type {import('next').NextConfig} */
const nextConfig = {
    output: 'standalone',
    reactStrictMode: true,
    transpilePackages: ['three', '@react-three/fiber', '@react-three/drei'],
    images: {
        loader: 'custom',
        loaderFile: './src/lib/safe-image-loader.ts',
    },
    // PWA headers
    async headers() {
        return [
            {
                source: '/(.*)',
                headers: [
                    { key: 'Cross-Origin-Opener-Policy', value: 'same-origin-allow-popups' },
                    { key: 'Cross-Origin-Resource-Policy', value: 'same-origin' },
                    { key: 'Permissions-Policy', value: 'camera=(self), clipboard-read=(self), clipboard-write=(self), geolocation=(), microphone=(), payment=(), usb=()' },
                    { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
                    { key: 'Strict-Transport-Security', value: 'max-age=63072000; includeSubDomains; preload' },
                    { key: 'X-Content-Type-Options', value: 'nosniff' },
                    { key: 'X-Frame-Options', value: 'DENY' },
                ],
            },
            {
                source: '/sw.js',
                headers: [
                    { key: 'Cache-Control', value: 'public, max-age=0, must-revalidate' },
                    { key: 'Service-Worker-Allowed', value: '/' },
                ],
            },
            {
                source: '/progressier.js',
                headers: [
                    { key: 'Cache-Control', value: 'public, max-age=0, must-revalidate' },
                    { key: 'Service-Worker-Allowed', value: '/' },
                    { key: 'Content-Type', value: 'text/javascript; charset=utf-8' },
                ],
            },
            {
                source: '/manifest.json',
                headers: [
                    { key: 'Content-Type', value: 'application/manifest+json' },
                ],
            },
        ];
    },
    webpack: (config) => {
        // Polyfill for wallet-core crypto deps
        config.resolve.fallback = {
            ...config.resolve.fallback,
            crypto: false,
            stream: false,
            buffer: false,
        };
        return config;
    },
};

export default withSentryConfig(nextConfig, {
    org: 'syncron-labs-ltd',
    project: 'paxport-wallet',
    // Upload source maps in CI only — avoids slowing local builds
    silent: true,
    widenClientFileUpload: true,
    hideSourceMaps: true,
    disableLogger: true,
    automaticVercelMonitors: false,
});
