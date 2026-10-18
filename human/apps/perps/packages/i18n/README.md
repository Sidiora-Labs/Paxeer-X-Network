# @paxeer-network/i18n

Internationalization and CLI tools for paxeer-network SDK. Based on i18next ecosystem.

## Quick start

Install the package, wrap your app root with `LocaleProvider`, and you get English by default.

```tsx
import { LocaleProvider } from "@paxeer-network/i18n";

export function App() {
  return (
    <LocaleProvider>
      <YourApp />
    </LocaleProvider>
  );
}
```

For multiple locales, custom keys, Vite bundling, HTTP loading, or the CLI, see the **Documentation** links below.

## Documentation

| Topic                                                             | Link                                                     |
| ----------------------------------------------------------------- | -------------------------------------------------------- |
| AI / Agent integration                                            | [docs/guide/AGENTS.md](./docs/guide/AGENTS.md)           |
| Package exports                                                   | [docs/guide/exports.md](./docs/guide/exports.md)         |
| Integration (providers, locales, extend keys, external resources) | [docs/guide/integration.md](./docs/guide/integration.md) |
| Utils (paths and locale helpers)                                  | [docs/guide/utils.md](./docs/guide/utils.md)             |
| Examples (recommended AsyncResources + alternatives)              | [docs/guide/examples.md](./docs/guide/examples.md)       |
| CLI (`i18n` binary)                                               | [docs/guide/cli.md](./docs/guide/cli.md)                 |

**Suggested reading order:** Agents start with [AGENTS](./docs/guide/AGENTS.md). Otherwise: [Integration](./docs/guide/integration.md) (API and behavior) → [Examples](./docs/guide/examples.md) (copy-paste recipes as needed) → [CLI](./docs/guide/cli.md) / [Utils](./docs/guide/utils.md) when relevant.

Generated API notes for `src/` live under `packages/i18n/docs/` in the monorepo only (not published in the npm package). The **user guide** under `docs/guide/` is included in the package so links above work after `npm install`.

### Package exports (summary)

| Export                            | Description                                                                                                  |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `@paxeer-network/i18n`           | Main entry: providers, hooks, `i18n`, resource helpers, types — see [exports guide](./docs/guide/exports.md) |
| `@paxeer-network/i18n/locales/*` | Built-in locale JSON files                                                                                   |
| `@paxeer-network/i18n/constant`  | Constants (`LocaleEnum`, `defaultLanguages`, …)                                                              |
| `@paxeer-network/i18n/utils`     | Utilities — see [Utils](./docs/guide/utils.md)                                                               |

The default i18n namespace is **`translation`** (`defaultNS`).
