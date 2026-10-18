# Utils (integrator-facing)

The `@paxeer-network/i18n/utils` entry exports the following helpers:

| Function                    | Purpose                                                                              |
| --------------------------- | ------------------------------------------------------------------------------------ |
| `parseI18nLang`             | Map browser language strings to a `LocaleCode` (from `LocaleEnum` or a custom list). |
| `removeLangPrefix`          | Remove a leading locale segment from a pathname.                                     |
| `getLocalePathFromPathname` | Read the locale segment from a pathname when present.                                |
| `generatePath`              | Build a locale-prefixed path with optional search string.                            |

For generated API notes on `utils.ts`, see [`../utils.md`](../utils.md) in this package’s docs.

See also: [Integration guide](./integration.md) · [Examples](./examples.md)
