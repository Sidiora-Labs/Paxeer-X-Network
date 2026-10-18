# api

## Overview

HTTP client and vault strategy API: request client with retry/interceptors, vault info/LP/overall endpoints, and environment-specific base URLs.

## Files

| File | Language | Description |
|------|----------|-------------|
| [api](./api.md) | TypeScript | getVaultInfo, getVaultLpPerformance, getVaultLpInfo, getVaultOverallInfo and response/param types |
| [request](./request.md) | TypeScript | RequestClient, fetch wrapper, interceptors, retry, VaultsApiError |
| [env](./env.md) | TypeScript | VAULTS_API_URLS, VAULTS_WEBSITE_URLS per env (prod/staging/qa/dev) |
