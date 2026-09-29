import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { withSentryConfig } from '@sentry/nextjs';

const appDir = path.dirname(fileURLToPath(import.meta.url));
const walletSdk = path.resolve(appDir, '../../wallet/sdk/src/index.ts');
const layerxSdk = path.resolve(appDir, '../../../agent/sdk/typescript/src/index.ts');
const layerxSdkBrowser = path.resolve(appDir, '../../../agent/sdk/typescript/src/browser.ts');
const walletSdkDir = path.resolve(appDir, '../../wallet/sdk');
const layerxSdkDir = path.resolve(appDir, '../../../agent/sdk/typescript');

/** @type {import('next').NextConfig} */
const nextConfig = {
    output: 'standalone',
    outputFileTracingRoot: path.resolve(appDir, '../../..'),
    reactStrictMode: true,
    transpilePackages: ['three', '@react-three/fiber', '@react-three/drei'],
    experimental: {
        externalDir: true,
        extensionAlias: {
            '.js': ['.js', '.ts', '.tsx'],
        },
    },
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
                source: '/manifest.json',
                headers: [
                    { key: 'Content-Type', value: 'application/manifest+json' },
                ],
            },
        ];
    },
    webpack: (config) => {
        // Polyfill for wallet-core crypto deps
        config.resolve.alias = {
            ...config.resolve.alias,
            '@paxeer/wallet$': walletSdk,
            '@sidiora/layerx-sdk/browser$': layerxSdkBrowser,
            '@sidiora/layerx-sdk$': layerxSdk,
        };
        config.module.rules.push({
            test: /\.(m?js|tsx?)$/,
            include: [walletSdkDir, layerxSdkDir],
            exclude: /node_modules/,
            resolve: {
                modules: [path.resolve(appDir, 'node_modules'), ...(config.resolve.modules ?? ['node_modules'])],
            },
        });
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
