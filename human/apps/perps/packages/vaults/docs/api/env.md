# env

## Overview

Environment-based base URLs for vault API and vault app website. Used by hooks and links (e.g. “View more” to app).

## Exports

| Name | Type | Description |
|------|------|-------------|
| `VAULTS_API_URLS` | `Record<Env, string>` | API base URL per env (prod, staging, qa, dev) |
| `VAULTS_WEBSITE_URLS` | `Record<Env, string>` | App base URL per env |

`Env` = `"prod" | "staging" | "qa" | "dev"`.

## Usage example

```typescript
import { VAULTS_API_URLS, VAULTS_WEBSITE_URLS } from "./env";
const apiUrl = VAULTS_API_URLS.prod;   // "https://api.paxeer.network"
const appUrl = VAULTS_WEBSITE_URLS.prod; // "https://app.paxeer.network"
```
