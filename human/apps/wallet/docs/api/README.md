# API Documentation

This directory contains API documentation for internal and external APIs.

## Internal API Routes

### `/api/sidiora/metadata`
Proxies Sidiora metadata API to avoid CORS/ORB issues.

**Method**: POST
**Rate Limit**: 100 requests per minute per IP

**Request Body**:
```json
{
  "addresses": ["0x...", "0x..."]
}
```

### `/api/sidiora/logo/[...path]`
Proxies Sidiora logo API for token images.

**Method**: GET
**Rate Limit**: 200 requests per minute per IP
**Cache**: 30 days

### `/api/sdk/[...path]`
Proxies SDK API requests.

**Method**: GET, POST
**Rate Limit**: 100 requests per minute per IP

### `/api/wallet/[...path]`
Proxies Blockscout wallet API for blockchain data.

**Method**: GET, POST
**Rate Limit**: 100 requests per minute per IP

## External APIs

### Sidiora
- **Metadata API**: `https://sidiora.fun/api/sdk/metadata/metadata/batch`
- **Logo API**: `https://sidiora.fun/api/sdk/metadata/logo/`
- **Purpose**: Token metadata and logos

### Crossverse
- **Data API**: `https://data-api.crossverse.app/api`
- **Purpose**: Price and candle data

### Blockscout
- **Base URL**: `https://paxscan.paxeer.app`
- **Purpose**: Blockchain explorer data

## Rate Limiting

All API proxy routes implement rate limiting:
- Default: 100 requests per minute per IP
- Logo endpoints: 200 requests per minute per IP
- Implemented using in-memory rate limiter

## Caching

- Static metadata: 24-hour localStorage cache
- Token logos: 30-day HTTP cache
- API responses: TanStack Query cache with configurable stale time
