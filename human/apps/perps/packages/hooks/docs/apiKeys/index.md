# apiKeys — Directory Index

## Directory Responsibility

Provides API key management for paxeer-network: listing, creating, and scoping API keys. Used by apps that need to manage trading API keys.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| index.ts | TS | Re-exports API key hooks and types | useApiKeyManager, APIKeyItem, ScopeType | [index.md](index.md) |
| useApiKeyManager.ts | TS | Hook for API key CRUD and scope management | useApiKeyManager, APIKeyItem, ScopeType | [useApiKeyManager.md](useApiKeyManager.md) |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| useApiKeyManager | useApiKeyManager.ts | Load/create/delete API keys, scope type |
| APIKeyItem | useApiKeyManager.ts | API key item shape |
| ScopeType | useApiKeyManager.ts | Scope type for keys |
