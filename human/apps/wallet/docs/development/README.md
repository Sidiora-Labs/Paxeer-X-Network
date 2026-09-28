# Development Guide

This directory contains development-related documentation.

## Getting Started

### Prerequisites

- Node.js 20 or higher
- npm or pnpm
- Git

### Installation

```bash
cd apps/wallet
pnpm install
```

### Development Server

```bash
pnpm dev
```

The app will be available at `http://localhost:3000`

## Code Style

We follow TypeScript strict mode and use ESLint/Prettier for code formatting.

### Running Linters

```bash
pnpm lint
pnpm typecheck
```

### Auto-fixing Issues

```bash
pnpm lint:fix
```

## Testing

```bash
pnpm test
```

## Building

```bash
pnpm build
```

## Environment Variables

Copy `.env.local.example` to `.env.local` and configure:

```bash
cp .env.local.example .env.local
```

Required variables:
- `NEXT_PUBLIC_SENTRY_DSN` - Sentry DSN for error tracking
- `NEXT_PUBLIC_RPC_URL` - RPC endpoint for blockchain interaction

## Debugging

### Chrome DevTools

1. Open DevTools (F12)
2. Use React DevTools for component inspection
3. Use Redux DevTools for state inspection (if applicable)
4. Check Console for errors and warnings

### Sentry

Errors are automatically reported to Sentry. Check the Sentry dashboard for production issues.

## Common Issues

### Port Already in Use

```bash
# Kill process on port 3000
npx kill-port 3000
```

### Clear Cache

```bash
rm -rf .next
rm -rf node_modules
pnpm install
```

### Type Errors

```bash
# Regenerate TypeScript build info
rm tsconfig.tsbuildinfo
pnpm run type-check
```
