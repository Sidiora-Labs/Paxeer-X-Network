# Deployment Guide

This directory contains deployment-related documentation.

## Overview

The Paxport Wallet can be deployed to various platforms including Vercel, Netlify, and self-hosted environments.

## Vercel Deployment

### Automatic Deployment

1. Connect your GitHub repository to Vercel
2. Set environment variables in Vercel dashboard
3. Deploy on push to main branch

### Manual Deployment

```bash
npm run build
vercel --prod
```

### Environment Variables

Set these in Vercel dashboard:
- `NEXT_PUBLIC_SENTRY_DSN`
- `SENTRY_DSN`
- `NEXT_PUBLIC_RPC_URL`
- `NEXT_PUBLIC_SIDIORA_API_URL`
- `NEXT_PUBLIC_CROSSVERSE_API_URL`
- `NEXT_PUBLIC_BLOCKSCOUT_API_URL`

## Docker Deployment

### Build Image

```bash
docker build -f docker/wallet-pwa/Dockerfile -t paxport-wallet .
```

### Run Container

```bash
docker run -p 3000:3000 \
  -e NEXT_PUBLIC_SENTRY_DSN=your-dsn \
  -e NEXT_PUBLIC_RPC_URL=your-rpc-url \
  paxport-wallet
```

### Docker Compose

```yaml
version: '3.8'
services:
  wallet:
    build: .
    ports:
      - "3000:3000"
    environment:
      - NEXT_PUBLIC_SENTRY_DSN=${SENTRY_DSN}
      - NEXT_PUBLIC_RPC_URL=${RPC_URL}
```

## Environment-Specific Builds

### Development

```bash
NODE_ENV=development npm run build
```

### Production

```bash
NODE_ENV=production npm run build
```

## Post-Deployment Checklist

- [ ] Verify environment variables are set
- [ ] Check build output for errors
- [ ] Test critical user flows
- [ ] Verify Sentry integration
- [ ] Check rate limiting is working
- [ ] Test API proxy routes
- [ ] Verify CORS settings

## Monitoring

- Check Sentry for errors
- Monitor API rate limits
- Track build deployment status
- Review performance metrics
