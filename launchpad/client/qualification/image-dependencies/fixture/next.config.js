module.exports = {
  images: {
    remotePatterns: [{ protocol: 'http', hostname: '127.0.0.1', port: process.env.IMAGE_FIXTURE_ORIGIN_PORT, pathname: '/allowed/**', search: '' }],
    dangerouslyAllowSVG: false,
    maximumResponseBody: 8388608,
    contentDispositionType: 'attachment',
    contentSecurityPolicy: "default-src 'self'; script-src 'none'; sandbox;",
    formats: ['image/avif', 'image/webp'],
  },
  experimental: { imgOptConcurrency: 2, imgOptMaxInputPixels: 16777216, imgOptTimeoutInSeconds: 7 },
};
