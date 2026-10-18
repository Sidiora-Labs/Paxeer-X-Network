# api – directory index

## Directory responsibility

API key management: list, create, edit, delete API keys. Exposes APIManagerWidget and APIManagerPage for the host app.

## Key entities

| Entity | File | Responsibility |
|--------|------|----------------|
| APIManagerWidget | apiManager.widget.tsx | Widget container for API manager UI |
| APIManagerPage | apiManager.page.tsx | Page component |
| apiManager.ui | apiManager.ui.tsx | Main UI layout |
| apiManager.script | apiManager.script.tsx | Script/state for API keys |
| createApiKey | dialog/createApiKey.tsx | Create API key dialog |
| editApiKey | dialog/editApiKey.tsx | Edit API key dialog |
| deleteApiKey | dialog/deleteApiKey.tsx | Delete confirmation dialog |
| createdApiKey | dialog/createdApiKey.tsx | Post-create display |

## Files

| File | Language | Link |
|------|----------|------|
| index.ts | TypeScript | [index.md](index.md) |
| apiManager.widget.tsx | TSX | [apiManager.widget.md](apiManager.widget.md) |
| apiManager.page.tsx | TSX | [apiManager.page.md](apiManager.page.md) |
| apiManager.ui.tsx | TSX | [apiManager.ui.md](apiManager.ui.md) |
| apiManager.script.tsx | TSX | [apiManager.script.md](apiManager.script.md) |
| dialog/createApiKey.tsx | TSX | [dialog/createApiKey.md](dialog/createApiKey.md) |
| dialog/editApiKey.tsx | TSX | [dialog/editApiKey.md](dialog/editApiKey.md) |
| dialog/deleteApiKey.tsx | TSX | [dialog/deleteApiKey.md](dialog/deleteApiKey.md) |
| dialog/createdApiKey.tsx | TSX | [dialog/createdApiKey.md](dialog/createdApiKey.md) |

## Subdirectories

[dialog](dialog/index.md) – API key dialogs (create, edit, delete, created).
