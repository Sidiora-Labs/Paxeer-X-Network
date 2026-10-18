# pages/dashboard

## Directory Responsibility

Dashboard page and tab: switches between affiliate and trader content. Exports Dashboard namespace (Page, Tab, TabWidget, useTabScript) for app entry.

## Files

| File | Language | Summary | Link |
|------|----------|---------|------|
| index.tsx | TSX | Re-exports dashboard page and tab | — |
| page.tsx | TSX | Dashboard page component | [page.md](page.md) |
| tab/tab.ui.tsx | TSX | Tab UI | tab/tab.ui.md |
| tab/tab.widget.tsx | TSX | Tab widget | tab/tab.widget.md |
| tab/tab.script.tsx | TSX | useTabScript | tab/tab.script.md |
| tab/index.ts | TS | Re-exports Tab, TabWidget, useTabScript | — |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| Tab | tab/tab.ui.tsx | Tab bar UI |
| TabWidget | tab/tab.widget.tsx | Tab with script |
| useTabScript | tab/tab.script.tsx | Tab state and handlers |
