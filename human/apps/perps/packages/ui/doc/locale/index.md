# locale — Directory Index

## Directory Responsibility

Locale context, provider, and useLocale for i18n; includes default en locale.

## Files

| File         | Language   | Summary                   | Link                         |
| ------------ | ---------- | ------------------------- | ---------------------------- |
| context.ts   | TypeScript | Locale context            | [context.md](context.md)     |
| en.ts        | TypeScript | English locale strings    | [en.md](en.md)               |
| index.ts     | TypeScript | Re-exports locale API     | [index.md](index.md)         |
| provider.tsx | TSX        | Locale provider component | [provider.md](provider.md)   |
| useLocale.ts | TypeScript | useLocale hook            | [useLocale.md](useLocale.md) |

## Key Entities

| Entity          | File         | Role                        |
| --------------- | ------------ | --------------------------- |
| Locale context  | context.ts   | Context for current locale  |
| Locale provider | provider.tsx | Wraps app to provide locale |
| useLocale       | useLocale.ts | Hook to access locale       |
